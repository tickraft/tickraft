// Copyright © 2026 Beijing Zhichuan Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package httputil

// HTTP header constants. All custom headers use the X-Tickraft- prefix.
const (
	// HeaderRequestID is the request tracing header, set by the RequestID
	// middleware and echoed in every response.
	HeaderRequestID = "X-Tickraft-Request-Id"

	// HeaderAPIKey is the API key authentication header, validated by the
	// APIKeyAuth middleware.
	HeaderAPIKey = "X-Tickraft-API-Key" //nolint:gosec // header name constant, not a credential

	// HeaderAssetKey is the asset key header used to authenticate telemetry
	// report endpoints, validated by the AssetKey middleware.
	HeaderAssetKey = "X-Tickraft-Asset-Key"

	// HeaderLocale is the request locale header carrying a BCP 47 language
	// tag, parsed by the Locale middleware.
	HeaderLocale = "X-Tickraft-Locale"

	// HeaderSignature is the webhook authentication header carrying the
	// hex-encoded HMAC-SHA256 of the raw request body.
	HeaderSignature = "X-Tickraft-Signature"
)
