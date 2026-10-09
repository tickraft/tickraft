// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package http

import (
	"bytes"
	"context"
	"errors"
	"mime"
	nethttp "net/http"
	"slices"
	"strconv"
	"strings"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"

	"github.com/tickraft/tickraft/pkg/quota"
	"github.com/tickraft/tickraft/pkg/telemetry"
)

// isExpositionContentType reports whether the request Content-Type selects
// the Prometheus text exposition branch (media type text/plain, typically
// sent as "text/plain; version=0.0.4; charset=utf-8"). An absent or
// unparsable Content-Type falls through to the JSON path.
func isExpositionContentType(header string) bool {
	mediaType, _, err := mime.ParseMediaType(header)
	return err == nil && mediaType == "text/plain"
}

// handleExposition ingests a Prometheus text exposition payload as a
// metrics-kind telemetry report. The body is parsed with the exposition
// text parser and flattened into the name→value metric map used by the
// ingest pipeline; sample timestamps in the payload are ignored and
// CollectedAt is stamped at receive time. Asset identity arrives via the
// query string (asset_id, or asset_key + tenant_id); a per-point
// signature owner binds the report exactly as on the JSON path. The raw
// body size is capped at the metrics-kind limit.
func (h *Listener) handleExposition(
	w nethttp.ResponseWriter,
	r *nethttp.Request,
	body []byte,
	sigOwner *SecretOwner,
	ingest func(context.Context, *telemetry.Telemetry) error,
) {
	if len(body) > maxMetricsBodySize {
		nethttp.Error(w, "request body too large", nethttp.StatusRequestEntityTooLarge)
		return
	}

	metrics, err := parseExposition(body)
	if err != nil {
		nethttp.Error(w, "invalid exposition payload: "+err.Error(), nethttp.StatusBadRequest)
		return
	}
	if len(metrics) == 0 {
		nethttp.Error(w, "invalid exposition payload: no samples", nethttp.StatusBadRequest)
		return
	}

	q := r.URL.Query()
	req := &reportRequest{
		AssetID:  queryID(q.Get("asset_id")),
		AssetKey: q.Get("asset_key"),
		TenantID: queryID(q.Get("tenant_id")),
		Metrics:  metrics,
	}
	report, status, ok := h.resolveTelemetry(r.Context(), req, body, r.RemoteAddr, sigOwner)
	if !ok {
		nethttp.Error(w, "asset not found", status)
		return
	}

	ceiling := quota.Ceiling(quota.TypeDailyEvents)
	if !h.counter.Allow(ceiling) {
		nethttp.Error(w, "daily event quota exceeded", nethttp.StatusTooManyRequests)
		return
	}

	h.accept(r.Context(), w, report, ingest)
}

// accept forwards the report to the ingest callback and acknowledges the
// push. Both the JSON and the exposition paths end here so the delivery
// semantics cannot drift apart; quota is evaluated by each path before
// calling it.
//
// The callback's error maps onto the response: an error wrapping
// telemetry.ErrIngestRejected rejects the push with 429 (plus Retry-After)
// so admission gates can apply backpressure, while any other error is
// logged and the push still acknowledged — the kernel itself ingests
// best-effort and has no rejection semantics of its own.
func (h *Listener) accept(
	ctx context.Context,
	w nethttp.ResponseWriter,
	report *telemetry.Telemetry,
	ingest func(context.Context, *telemetry.Telemetry) error,
) {
	if ingest != nil {
		if err := ingest(ctx, report); err != nil {
			if errors.Is(err, telemetry.ErrIngestRejected) {
				w.Header().Set("Retry-After", "1")
				nethttp.Error(w, "ingest rejected", nethttp.StatusTooManyRequests)
				return
			}
			h.logger.Warn("http listener: ingest callback failed",
				zap.Error(err),
			)
		}
	}
	w.WriteHeader(nethttp.StatusAccepted)
}

// queryID parses a decimal query-string integer, returning 0 on absence or
// a malformed value (the asset resolution treats 0 as unset).
func queryID(raw string) int64 {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return id
}

// parseExposition parses a Prometheus text exposition payload (format
// version 0.0.4) into the flat name→value metric map used by the ingest
// pipeline. Label sets fold into the key using the canonical
// name{label="value",...} rendering; histograms and summaries expand into
// their _sum/_count plus per-boundary (_bucket{le=...}) and per-quantile
// ({quantile=...}) series.
func parseExposition(body []byte) (map[string]float64, error) {
	// UTF8Validation is the permissive scheme: it accepts both legacy
	// ASCII and UTF-8 metric/label names, which suits an ingest edge that
	// must not reject what the reporter's Prometheus client emitted.
	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64)
	for name, family := range families {
		for _, m := range family.GetMetric() {
			key := labelKey(name, m.GetLabel())
			switch {
			case m.GetGauge() != nil:
				out[key] = m.GetGauge().GetValue()
			case m.GetCounter() != nil:
				out[key] = m.GetCounter().GetValue()
			case m.GetUntyped() != nil:
				out[key] = m.GetUntyped().GetValue()
			case m.GetHistogram() != nil:
				hist := m.GetHistogram()
				out[key+"_sum"] = hist.GetSampleSum()
				out[key+"_count"] = float64(hist.GetSampleCount())
				for _, bucket := range hist.GetBucket() {
					le := formatFloat(bucket.GetUpperBound())
					out[key+"_bucket{le=\""+le+"\"}"] = float64(bucket.GetCumulativeCount())
				}
			case m.GetSummary() != nil:
				summary := m.GetSummary()
				out[key+"_sum"] = summary.GetSampleSum()
				out[key+"_count"] = float64(summary.GetSampleCount())
				for _, quantile := range summary.GetQuantile() {
					phi := formatFloat(quantile.GetQuantile())
					out[key+"{quantile=\""+phi+"\"}"] = quantile.GetValue()
				}
			}
		}
	}
	return out, nil
}

// labelKey renders the canonical exposition series identifier for a sample:
// the family name plus, when labeled, a comma-separated label list sorted by
// label name (e.g. http_requests_total{code="200",method="get"}). Sorting
// keeps the key deterministic regardless of the label order on the wire.
func labelKey(name string, labels []*dto.LabelPair) string {
	if len(labels) == 0 {
		return name
	}
	sorted := make([]*dto.LabelPair, len(labels))
	copy(sorted, labels)
	slices.SortFunc(sorted, func(a, b *dto.LabelPair) int {
		return strings.Compare(a.GetName(), b.GetName())
	})
	var sb strings.Builder
	sb.WriteString(name)
	sb.WriteByte('{')
	for i, pair := range sorted {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(pair.GetName())
		sb.WriteString(`="`)
		sb.WriteString(pair.GetValue())
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

// formatFloat renders a boundary or quantile value the way the exposition
// format spells it (minimal digits, "+Inf" for infinity).
func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'g', -1, 64)
}
