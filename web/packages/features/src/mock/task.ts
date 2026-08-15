// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

import type { MockMethod } from './types'

/**
 * Task dataset (22 items, covering 5 open-source executors and 4 schedule types)
 * Aligned with storyboard mock-data.js
 */
const mockTasks = [
  {
    id: 1, tenant_id: 1, asset_id: 1, name: 'prod-web-01 Health Check', description: 'Probe prod-web-01 health check endpoint every minute',
    executor_type: 'http', executor_config: '{"url":"http://prod-web-01:8080/health","method":"GET","expect_code":200}',
    schedule_type: 'interval', cron_expr: '', interval: 60, timeout: 10, priority: 'medium',
    depends_on: [], max_retries: 3, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 13:58:00', next_run: '2026-06-30 13:59:00',
    created_at: '2026-06-01 10:00:00', updated_at: '2026-06-15 09:30:00',
  },
  {
    id: 2, tenant_id: 1, asset_id: 5, name: 'Database Master-Slave Sync Check', description: 'Check MySQL master-slave port connectivity every 5 minutes',
    executor_type: 'tcp', executor_config: '{"host":"prod-db-02","port":3306}',
    schedule_type: 'cron', cron_expr: '*/5 * * * *', interval: 0, timeout: 5, priority: 'medium',
    depends_on: [], max_retries: 2, retry_interval: 10, enabled: true,
    last_run: '2026-06-30 13:55:00', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-02 11:20:00', updated_at: '2026-06-12 14:00:00',
  },
  {
    id: 3, tenant_id: 1, asset_id: 10, name: 'CDN Node ICMP Probe', description: 'Ping CDN node every 30 seconds to measure latency',
    executor_type: 'icmp', executor_config: '{"host":"cdn-edge-01.tickraft.io","count":4}',
    schedule_type: 'interval', cron_expr: '', interval: 30, timeout: 3, priority: 'low',
    depends_on: [], max_retries: 5, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 13:59:30', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-03 09:00:00', updated_at: '2026-06-20 16:30:00',
  },
  {
    id: 4, tenant_id: 1, asset_id: 15, name: 'Log Archive Script', description: 'Archive previous day logs at 02:00 every day',
    executor_type: 'local', executor_config: '{"command":"/opt/tickraft/scripts/archive_logs.sh","workdir":"/var/log"}',
    schedule_type: 'cron', cron_expr: '0 2 * * *', interval: 0, timeout: 600, priority: 'low',
    depends_on: [], max_retries: 1, retry_interval: 60, enabled: true,
    last_run: '2026-06-30 02:00:00', next_run: '2026-07-01 02:00:00',
    created_at: '2026-05-28 18:00:00', updated_at: '2026-06-25 09:15:00',
  },
  {
    id: 5, tenant_id: 1, asset_id: 2, name: 'Webhook Alert Forwarding', description: 'Trigger Webhook notification when dependency task 1 fails',
    executor_type: 'webhook', executor_config: '{"url":"https://hooks.example.com/alert","method":"POST","secret":"wh_*****"}',
    schedule_type: 'event', cron_expr: '', interval: 0, timeout: 15, priority: 'medium',
    depends_on: [1], max_retries: 3, retry_interval: 10, enabled: true,
    last_run: '2026-06-30 13:30:00', next_run: '',
    created_at: '2026-06-05 14:00:00', updated_at: '2026-06-22 11:45:00',
  },
  {
    id: 6, tenant_id: 1, asset_id: 7, name: 'Redis Cache Cleanup', description: 'Clean expired cache keys every 6 hours',
    executor_type: 'local', executor_config: '{"command":"redis-cli --scan --pattern \\"tmp:*\\" | xargs redis-cli del"}',
    schedule_type: 'cron', cron_expr: '0 */6 * * *', interval: 0, timeout: 120, priority: 'low',
    depends_on: [], max_retries: 2, retry_interval: 30, enabled: true,
    last_run: '2026-06-30 12:00:00', next_run: '2026-06-30 18:00:00',
    created_at: '2026-05-30 10:00:00', updated_at: '2026-06-18 13:00:00',
  },
  {
    id: 7, tenant_id: 1, asset_id: 3, name: 'prod-api-03 API Stress Test', description: 'One-time API stress test task (disabled)',
    executor_type: 'http', executor_config: '{"url":"http://prod-api-03:8080/benchmark","method":"POST","concurrency":50}',
    schedule_type: 'once', cron_expr: '', interval: 0, timeout: 300, priority: 'low',
    depends_on: [], max_retries: 0, retry_interval: 0, enabled: false,
    last_run: '2026-06-28 10:00:00', next_run: '',
    created_at: '2026-06-25 14:30:00', updated_at: '2026-06-28 11:00:00',
  },
  {
    id: 8, tenant_id: 1, asset_id: 15, name: 'Backup Storage Upload to OSS', description: 'Upload archived logs to OSS at 03:00 every Sunday',
    executor_type: 'local', executor_config: '{"command":"/opt/tickraft/scripts/upload_oss.sh --bucket=backup"}',
    schedule_type: 'cron', cron_expr: '0 3 * * 0', interval: 0, timeout: 1800, priority: 'medium',
    depends_on: [4], max_retries: 1, retry_interval: 120, enabled: true,
    last_run: '2026-06-29 03:00:00', next_run: '2026-07-06 03:00:00',
    created_at: '2026-05-25 09:00:00', updated_at: '2026-06-26 10:00:00',
  },
  {
    id: 9, tenant_id: 1, asset_id: 11, name: 'Certificate Expiry Check', description: 'Check HTTPS certificate validity at 09:00 every day',
    executor_type: 'http', executor_config: '{"url":"https://www.tickraft.io","method":"GET","check_cert":true}',
    schedule_type: 'cron', cron_expr: '0 9 * * *', interval: 0, timeout: 30, priority: 'low',
    depends_on: [], max_retries: 3, retry_interval: 60, enabled: true,
    last_run: '2026-06-30 09:00:00', next_run: '2026-07-01 09:00:00',
    created_at: '2026-06-01 09:00:00', updated_at: '2026-06-19 16:00:00',
  },
  {
    id: 10, tenant_id: 1, asset_id: 6, name: 'PG Port Listen Check', description: 'Check PostgreSQL 5432 port every 2 minutes',
    executor_type: 'tcp', executor_config: '{"host":"prod-db-03","port":5432}',
    schedule_type: 'interval', cron_expr: '', interval: 120, timeout: 5, priority: 'low',
    depends_on: [], max_retries: 3, retry_interval: 10, enabled: true,
    last_run: '2026-06-30 13:58:00', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-04 10:00:00', updated_at: '2026-06-21 14:30:00',
  },
  {
    id: 11, tenant_id: 1, asset_id: 12, name: 'Intranet Gateway ICMP Probe', description: 'Ping intranet gateway every minute',
    executor_type: 'icmp', executor_config: '{"host":"10.0.0.1","count":4}',
    schedule_type: 'interval', cron_expr: '', interval: 60, timeout: 3, priority: 'medium',
    depends_on: [], max_retries: 5, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 13:59:00', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-06 11:00:00', updated_at: '2026-06-23 09:00:00',
  },
  {
    id: 12, tenant_id: 1, asset_id: 13, name: 'Config File Sync', description: 'Triggered by config change events, sync to all nodes (disabled)',
    executor_type: 'local', executor_config: '{"command":"/opt/tickraft/scripts/sync_config.sh"}',
    schedule_type: 'event', cron_expr: '', interval: 0, timeout: 60, priority: 'low',
    depends_on: [], max_retries: 2, retry_interval: 30, enabled: false,
    last_run: '2026-06-27 15:00:00', next_run: '',
    created_at: '2026-05-29 14:00:00', updated_at: '2026-06-27 15:30:00',
  },
  {
    id: 13, tenant_id: 1, asset_id: 4, name: 'Payment Callback API Check', description: 'Check payment callback API availability every 10 minutes',
    executor_type: 'http', executor_config: '{"url":"https://api.tickraft.io/pay/callback","method":"GET"}',
    schedule_type: 'cron', cron_expr: '*/10 * * * *', interval: 0, timeout: 8, priority: 'medium',
    depends_on: [], max_retries: 3, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 13:50:00', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-07 13:00:00', updated_at: '2026-06-24 16:00:00',
  },
  {
    id: 14, tenant_id: 1, asset_id: 8, name: 'Kafka Cluster Port Check', description: 'Check Kafka 9092 port every 90 seconds',
    executor_type: 'tcp', executor_config: '{"host":"prod-kafka-01","port":9092}',
    schedule_type: 'interval', cron_expr: '', interval: 90, timeout: 5, priority: 'low',
    depends_on: [], max_retries: 3, retry_interval: 10, enabled: true,
    last_run: '2026-06-30 13:58:30', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-08 10:30:00', updated_at: '2026-06-25 11:00:00',
  },
  {
    id: 15, tenant_id: 1, asset_id: 8, name: 'Message Queue Backlog Alert', description: 'Forward alert to WeCom when Kafka port check fails',
    executor_type: 'webhook', executor_config: '{"url":"https://qyapi.weixin.qq.com/cgi-bin/webhook/send","method":"POST"}',
    schedule_type: 'event', cron_expr: '', interval: 0, timeout: 10, priority: 'medium',
    depends_on: [14], max_retries: 5, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 12:30:00', next_run: '',
    created_at: '2026-06-09 14:00:00', updated_at: '2026-06-26 09:30:00',
  },
  {
    id: 16, tenant_id: 1, asset_id: 15, name: 'Disk Space Check', description: 'Check disk usage every hour',
    executor_type: 'local', executor_config: '{"command":"df -h | awk \'$5 > 80 {print}\'","threshold":80}',
    schedule_type: 'cron', cron_expr: '0 * * * *', interval: 0, timeout: 30, priority: 'low',
    depends_on: [], max_retries: 1, retry_interval: 60, enabled: true,
    last_run: '2026-06-30 13:00:00', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-10 09:00:00', updated_at: '2026-06-27 15:00:00',
  },
  {
    id: 17, tenant_id: 1, asset_id: 11, name: 'HTTPS Certificate Monitor', description: 'Scan all HTTPS site certificates at 00:00 every day',
    executor_type: 'http', executor_config: '{"url":"https://certs.tickraft.io/scan","method":"GET"}',
    schedule_type: 'cron', cron_expr: '0 0 * * *', interval: 0, timeout: 60, priority: 'low',
    depends_on: [], max_retries: 2, retry_interval: 60, enabled: true,
    last_run: '2026-06-30 00:00:00', next_run: '2026-07-01 00:00:00',
    created_at: '2026-06-11 10:00:00', updated_at: '2026-06-28 16:00:00',
  },
  {
    id: 18, tenant_id: 1, asset_id: 5, name: 'Local Database Backup', description: 'Backup database at 04:00 every day',
    executor_type: 'local', executor_config: '{"command":"/opt/tickraft/scripts/backup_db.sh --compress"}',
    schedule_type: 'cron', cron_expr: '0 4 * * *', interval: 0, timeout: 900, priority: 'medium',
    depends_on: [], max_retries: 2, retry_interval: 120, enabled: true,
    last_run: '2026-06-30 04:00:00', next_run: '2026-07-01 04:00:00',
    created_at: '2026-05-26 09:00:00', updated_at: '2026-06-29 10:00:00',
  },
  {
    id: 19, tenant_id: 1, asset_id: 14, name: 'Host Memory Monitor', description: 'Collect host memory metrics every 30 seconds',
    executor_type: 'local', executor_config: '{"command":"free -m | awk \'/Mem/{print $3}\'}',
    schedule_type: 'interval', cron_expr: '', interval: 30, timeout: 5, priority: 'low',
    depends_on: [], max_retries: 3, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 13:59:30', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-12 14:00:00', updated_at: '2026-06-29 15:00:00',
  },
  {
    id: 20, tenant_id: 1, asset_id: 7, name: 'Redis Master-Slave Switch Alert', description: 'Trigger master-slave switch alert when Redis cache cleanup fails',
    executor_type: 'webhook', executor_config: '{"url":"https://hooks.example.com/redis-alert","method":"POST"}',
    schedule_type: 'event', cron_expr: '', interval: 0, timeout: 10, priority: 'high',
    depends_on: [6], max_retries: 5, retry_interval: 5, enabled: true,
    last_run: '2026-06-29 18:00:00', next_run: '',
    created_at: '2026-06-13 10:00:00', updated_at: '2026-06-29 18:30:00',
  },
  {
    id: 21, tenant_id: 1, asset_id: 12, name: 'ICMP Gateway Latency Collection', description: 'Ping public network gateway every minute to collect latency',
    executor_type: 'icmp', executor_config: '{"host":"8.8.8.8","count":4}',
    schedule_type: 'cron', cron_expr: '*/1 * * * *', interval: 0, timeout: 3, priority: 'low',
    depends_on: [], max_retries: 3, retry_interval: 5, enabled: true,
    last_run: '2026-06-30 13:59:00', next_run: '2026-06-30 14:00:00',
    created_at: '2026-06-14 09:00:00', updated_at: '2026-06-30 13:59:00',
  },
  {
    id: 22, tenant_id: 1, asset_id: 9, name: 'ES Cluster Health Check', description: 'Check ES cluster health status every 15 minutes (disabled)',
    executor_type: 'http', executor_config: '{"url":"http://prod-es-01:9200/_cluster/health","method":"GET"}',
    schedule_type: 'cron', cron_expr: '*/15 * * * *', interval: 0, timeout: 10, priority: 'medium',
    depends_on: [], max_retries: 3, retry_interval: 30, enabled: false,
    last_run: '2026-06-29 09:00:00', next_run: '',
    created_at: '2026-06-15 14:00:00', updated_at: '2026-06-29 10:00:00',
  },
]

/**
 * Execution log dataset (32 items, covering 4 statuses, aligned with storyboard mock-data.js)
 */
const mockLogs = [
  { id: 1, tenant_id: 1, task_id: 1, asset_id: 1, task_name: 'prod-web-01 Health Check', executor_type: 'http', status: 'success', status_code: 200, output: 'HTTP 200, response time: 12ms', error_message: '', duration: 12, retry_count: 0, started_at: '2026-06-30 13:58:00', finished_at: '2026-06-30 13:58:01', created_at: '2026-06-30 13:58:00' },
  { id: 2, tenant_id: 1, task_id: 2, asset_id: 5, task_name: 'Database Master-Slave Sync Check', executor_type: 'tcp', status: 'success', status_code: 0, output: 'TCP connect success', error_message: '', duration: 5, retry_count: 0, started_at: '2026-06-30 13:55:00', finished_at: '2026-06-30 13:55:01', created_at: '2026-06-30 13:55:00' },
  { id: 3, tenant_id: 1, task_id: 3, asset_id: 10, task_name: 'CDN Node ICMP Probe', executor_type: 'icmp', status: 'success', status_code: 0, output: '4 packets transmitted, 4 received, 0% loss, avg 12.5ms', error_message: '', duration: 35, retry_count: 0, started_at: '2026-06-30 13:59:30', finished_at: '2026-06-30 13:59:35', created_at: '2026-06-30 13:59:30' },
  { id: 4, tenant_id: 1, task_id: 4, asset_id: 15, task_name: 'Log Archive Script', executor_type: 'local', status: 'success', status_code: 0, output: 'archived 1024 files, total size 5.2GB', error_message: '', duration: 320, retry_count: 0, started_at: '2026-06-30 02:00:00', finished_at: '2026-06-30 02:05:20', created_at: '2026-06-30 02:00:00' },
  { id: 5, tenant_id: 1, task_id: 5, asset_id: 2, task_name: 'Webhook Alert Forwarding', executor_type: 'webhook', status: 'success', status_code: 200, output: 'webhook delivered', error_message: '', duration: 80, retry_count: 0, started_at: '2026-06-30 13:30:00', finished_at: '2026-06-30 13:30:01', created_at: '2026-06-30 13:30:00' },
  { id: 6, tenant_id: 1, task_id: 6, asset_id: 7, task_name: 'Redis Cache Cleanup', executor_type: 'local', status: 'failed', status_code: 1, output: 'redis-cli: connection refused', error_message: 'Could not connect to Redis at 10.0.3.31:6379: Connection refused', duration: 2, retry_count: 2, started_at: '2026-06-30 12:00:00', finished_at: '2026-06-30 12:00:08', created_at: '2026-06-30 12:00:00' },
  { id: 7, tenant_id: 1, task_id: 7, asset_id: 3, task_name: 'prod-api-03 API Stress Test', executor_type: 'http', status: 'timeout', status_code: 0, output: '', error_message: 'request timeout after 300s', duration: 300000, retry_count: 0, started_at: '2026-06-28 10:00:00', finished_at: '2026-06-28 10:05:00', created_at: '2026-06-28 10:00:00' },
  { id: 8, tenant_id: 1, task_id: 8, asset_id: 15, task_name: 'Backup Storage Upload to OSS', executor_type: 'local', status: 'success', status_code: 0, output: 'uploaded 5.2GB to oss://backup/2026-06-30', error_message: '', duration: 480, retry_count: 0, started_at: '2026-06-29 03:00:00', finished_at: '2026-06-29 03:08:00', created_at: '2026-06-29 03:00:00' },
  { id: 9, tenant_id: 1, task_id: 9, asset_id: 11, task_name: 'Certificate Expiry Check', executor_type: 'http', status: 'success', status_code: 200, output: 'cert expires in 184 days', error_message: '', duration: 280, retry_count: 0, started_at: '2026-06-30 09:00:00', finished_at: '2026-06-30 09:00:28', created_at: '2026-06-30 09:00:00' },
  { id: 10, tenant_id: 1, task_id: 10, asset_id: 6, task_name: 'PG Port Listen Check', executor_type: 'tcp', status: 'success', status_code: 0, output: 'TCP connect success', error_message: '', duration: 5, retry_count: 0, started_at: '2026-06-30 13:58:00', finished_at: '2026-06-30 13:58:01', created_at: '2026-06-30 13:58:00' },
  { id: 11, tenant_id: 1, task_id: 11, asset_id: 12, task_name: 'Intranet Gateway ICMP Probe', executor_type: 'icmp', status: 'success', status_code: 0, output: 'avg 1.2ms', error_message: '', duration: 8, retry_count: 0, started_at: '2026-06-30 13:59:00', finished_at: '2026-06-30 13:59:01', created_at: '2026-06-30 13:59:00' },
  { id: 12, tenant_id: 1, task_id: 13, asset_id: 4, task_name: 'Payment Callback API Check', executor_type: 'http', status: 'failed', status_code: 500, output: '', error_message: 'HTTP 500 Internal Server Error', duration: 850, retry_count: 3, started_at: '2026-06-30 13:50:00', finished_at: '2026-06-30 13:50:03', created_at: '2026-06-30 13:50:00' },
  { id: 13, tenant_id: 1, task_id: 14, asset_id: 8, task_name: 'Kafka Cluster Port Check', executor_type: 'tcp', status: 'success', status_code: 0, output: 'TCP connect success', error_message: '', duration: 6, retry_count: 0, started_at: '2026-06-30 13:58:30', finished_at: '2026-06-30 13:58:31', created_at: '2026-06-30 13:58:30' },
  { id: 14, tenant_id: 1, task_id: 15, asset_id: 8, task_name: 'Message Queue Backlog Alert', executor_type: 'webhook', status: 'success', status_code: 200, output: 'webhook delivered', error_message: '', duration: 95, retry_count: 0, started_at: '2026-06-30 12:30:00', finished_at: '2026-06-30 12:30:01', created_at: '2026-06-30 12:30:00' },
  { id: 15, tenant_id: 1, task_id: 16, asset_id: 15, task_name: 'Disk Space Check', executor_type: 'local', status: 'success', status_code: 0, output: 'all disks under 80%', error_message: '', duration: 320, retry_count: 0, started_at: '2026-06-30 13:00:00', finished_at: '2026-06-30 13:00:04', created_at: '2026-06-30 13:00:00' },
  { id: 16, tenant_id: 1, task_id: 17, asset_id: 11, task_name: 'HTTPS Certificate Monitor', executor_type: 'http', status: 'success', status_code: 200, output: 'checked 12 sites', error_message: '', duration: 4200, retry_count: 0, started_at: '2026-06-30 00:00:00', finished_at: '2026-06-30 00:00:42', created_at: '2026-06-30 00:00:00' },
  { id: 17, tenant_id: 1, task_id: 18, asset_id: 5, task_name: 'Local Database Backup', executor_type: 'local', status: 'success', status_code: 0, output: 'backup size: 8.5GB', error_message: '', duration: 580, retry_count: 0, started_at: '2026-06-30 04:00:00', finished_at: '2026-06-30 04:09:40', created_at: '2026-06-30 04:00:00' },
  { id: 18, tenant_id: 1, task_id: 19, asset_id: 14, task_name: 'Host Memory Monitor', executor_type: 'local', status: 'success', status_code: 0, output: 'mem used: 5.2G/8G', error_message: '', duration: 8, retry_count: 0, started_at: '2026-06-30 13:59:30', finished_at: '2026-06-30 13:59:30', created_at: '2026-06-30 13:59:30' },
  { id: 19, tenant_id: 1, task_id: 20, asset_id: 7, task_name: 'Redis Master-Slave Switch Alert', executor_type: 'webhook', status: 'success', status_code: 200, output: 'webhook delivered', error_message: '', duration: 110, retry_count: 0, started_at: '2026-06-29 18:00:00', finished_at: '2026-06-29 18:00:01', created_at: '2026-06-29 18:00:00' },
  { id: 20, tenant_id: 1, task_id: 21, asset_id: 12, task_name: 'ICMP Gateway Latency Collection', executor_type: 'icmp', status: 'success', status_code: 0, output: 'avg 8.5ms', error_message: '', duration: 12, retry_count: 0, started_at: '2026-06-30 13:59:00', finished_at: '2026-06-30 13:59:01', created_at: '2026-06-30 13:59:00' },
  { id: 21, tenant_id: 1, task_id: 1, asset_id: 1, task_name: 'prod-web-01 Health Check', executor_type: 'http', status: 'success', status_code: 200, output: 'HTTP 200, response time: 15ms', error_message: '', duration: 15, retry_count: 0, started_at: '2026-06-30 13:57:00', finished_at: '2026-06-30 13:57:01', created_at: '2026-06-30 13:57:00' },
  { id: 22, tenant_id: 1, task_id: 22, asset_id: 9, task_name: 'ES Cluster Health Check', executor_type: 'http', status: 'failed', status_code: 0, output: '', error_message: 'dial tcp 10.0.5.51:9200: connect: connection refused', duration: 5, retry_count: 3, started_at: '2026-06-29 09:00:00', finished_at: '2026-06-29 09:00:30', created_at: '2026-06-29 09:00:00' },
  { id: 23, tenant_id: 1, task_id: 1, asset_id: 1, task_name: 'prod-web-01 Health Check', executor_type: 'http', status: 'success', status_code: 200, output: 'HTTP 200, response time: 10ms', error_message: '', duration: 10, retry_count: 0, started_at: '2026-06-30 13:56:00', finished_at: '2026-06-30 13:56:01', created_at: '2026-06-30 13:56:00' },
  { id: 24, tenant_id: 1, task_id: 13, asset_id: 4, task_name: 'Payment Callback API Check', executor_type: 'http', status: 'success', status_code: 200, output: 'HTTP 200', error_message: '', duration: 80, retry_count: 0, started_at: '2026-06-30 13:40:00', finished_at: '2026-06-30 13:40:01', created_at: '2026-06-30 13:40:00' },
  { id: 25, tenant_id: 1, task_id: 2, asset_id: 5, task_name: 'Database Master-Slave Sync Check', executor_type: 'tcp', status: 'success', status_code: 0, output: 'TCP connect success', error_message: '', duration: 5, retry_count: 0, started_at: '2026-06-30 13:50:00', finished_at: '2026-06-30 13:50:01', created_at: '2026-06-30 13:50:00' },
  { id: 26, tenant_id: 1, task_id: 14, asset_id: 8, task_name: 'Kafka Cluster Port Check', executor_type: 'tcp', status: 'success', status_code: 0, output: 'TCP connect success', error_message: '', duration: 6, retry_count: 0, started_at: '2026-06-30 13:57:00', finished_at: '2026-06-30 13:57:01', created_at: '2026-06-30 13:57:00' },
  { id: 27, tenant_id: 1, task_id: 3, asset_id: 10, task_name: 'CDN Node ICMP Probe', executor_type: 'icmp', status: 'success', status_code: 0, output: 'avg 13.2ms', error_message: '', duration: 35, retry_count: 0, started_at: '2026-06-30 13:59:00', finished_at: '2026-06-30 13:59:05', created_at: '2026-06-30 13:59:00' },
  { id: 28, tenant_id: 1, task_id: 6, asset_id: 7, task_name: 'Redis Cache Cleanup', executor_type: 'local', status: 'success', status_code: 0, output: 'cleaned 256 keys', error_message: '', duration: 120, retry_count: 0, started_at: '2026-06-30 06:00:00', finished_at: '2026-06-30 06:00:02', created_at: '2026-06-30 06:00:00' },
  { id: 29, tenant_id: 1, task_id: 16, asset_id: 15, task_name: 'Disk Space Check', executor_type: 'local', status: 'success', status_code: 0, output: 'all disks under 80%', error_message: '', duration: 280, retry_count: 0, started_at: '2026-06-30 12:00:00', finished_at: '2026-06-30 12:00:04', created_at: '2026-06-30 12:00:00' },
  { id: 30, tenant_id: 1, task_id: 19, asset_id: 14, task_name: 'Host Memory Monitor', executor_type: 'local', status: 'running', status_code: 0, output: '', error_message: '', duration: 0, retry_count: 0, started_at: '2026-06-30 14:00:00', finished_at: '', created_at: '2026-06-30 14:00:00' },
  { id: 31, tenant_id: 1, task_id: 9, asset_id: 11, task_name: 'Certificate Expiry Check', executor_type: 'http', status: 'success', status_code: 200, output: 'cert expires in 184 days', error_message: '', duration: 290, retry_count: 0, started_at: '2026-06-29 09:00:00', finished_at: '2026-06-29 09:00:29', created_at: '2026-06-29 09:00:00' },
  { id: 32, tenant_id: 1, task_id: 13, asset_id: 4, task_name: 'Payment Callback API Check', executor_type: 'http', status: 'failed', status_code: 503, output: '', error_message: 'HTTP 503 Service Unavailable', duration: 90, retry_count: 3, started_at: '2026-06-30 13:20:00', finished_at: '2026-06-30 13:20:03', created_at: '2026-06-30 13:20:00' },
]

// Asset list is handled by telemetry mock (/api/v1/assets)

/** Extract numeric ID from URL (compatible with multi-segment paths like /tasks/:id/trigger) */
function extractId(url: string): number {
  const match = url.match(/\/(\d+)(?:\/|$)/)
  return match ? Number(match[1]) : 0
}

/** Extract task ID from sub-resource URL like /api/v1/tasks/:id/executions */
function extractTaskId(url: string): number {
  const match = url.match(/\/tasks\/(\d+)\/executions/)
  return match ? Number(match[1]) : 0
}

/** Extract execution ID from sub-resource URL like /api/v1/tasks/:id/executions/:execId */
function extractExecId(url: string): number {
  const match = url.match(/\/executions\/(\d+)/)
  return match ? Number(match[1]) : 0
}

export default [
  // Task list
  {
    url: '/api/v1/tasks',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...mockTasks]
      if (query?.name) {
        filtered = filtered.filter((t) => t.name.includes(query.name))
      }
      if (query?.executor_type) {
        filtered = filtered.filter((t) => t.executor_type === query.executor_type)
      }
      if (query?.schedule_type) {
        filtered = filtered.filter((t) => t.schedule_type === query.schedule_type)
      }
      if (query?.enabled === 'true' || query?.enabled === 'false') {
        const target = query.enabled === 'true'
        filtered = filtered.filter((t) => t.enabled === target)
      }
      const total = filtered.length
      const start = (page - 1) * size
      const items = filtered.slice(start, start + size)
      return { code: 0, message: 'success', data: { items, total, page, page_size: size } }
    },
  },
  // Create task
  {
    url: '/api/v1/tasks',
    method: 'post',
    response: ({ body }: { body: Record<string, unknown> }) => ({
      code: 0,
      message: 'success',
      data: {
        id: mockTasks.length + 1,
        tenant_id: 1,
        asset_id: body.asset_id ?? 0,
        name: body.name ?? '',
        description: body.description ?? '',
        executor_type: body.executor_type ?? 'local',
        executor_config: body.executor_config ?? '{}',
        schedule_type: body.schedule_type ?? 'once',
        cron_expr: body.cron_expr ?? '',
        interval: body.interval ?? 0,
        timeout: body.timeout ?? 30,
        priority: body.priority ?? 'medium',
        depends_on: body.depends_on ?? [],
        max_retries: body.max_retries ?? 0,
        retry_interval: body.retry_interval ?? 0,
        enabled: body.enabled ?? true,
        metadata: body.metadata ?? '',
        last_run: '',
        next_run: '',
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      },
    }),
  },
  // Task detail
  {
    url: '/api/v1/tasks/:id',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id)
      return { code: 0, message: 'success', data: task || mockTasks[0] }
    },
  },
  // Update task
  {
    url: '/api/v1/tasks/:id',
    method: 'put',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id) || mockTasks[0]
      return { code: 0, message: 'success', data: { ...task, ...body, updated_at: new Date().toISOString() } }
    },
  },
  // Delete task
  {
    url: '/api/v1/tasks/:id',
    method: 'delete',
    response: () => ({ code: 0, message: 'success', data: null }),
  },
  // Trigger task
  {
    url: '/api/v1/tasks/:id/trigger',
    method: 'post',
    response: () => ({ code: 0, message: 'success', data: null }),
  },
  // Pause task (remove from scheduling wheel, config preserved)
  {
    url: '/api/v1/tasks/:id/pause',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id)
      if (task) {
        task.enabled = false
        task.next_run = ''
      }
      return { code: 0, message: 'success', data: null }
    },
  },
  // Resume a paused task
  {
    url: '/api/v1/tasks/:id/resume',
    method: 'post',
    response: ({ url }: { url: string }) => {
      const id = extractId(url)
      const task = mockTasks.find((t) => t.id === id)
      if (task) task.enabled = true
      return { code: 0, message: 'success', data: null }
    },
  },
  // Copy a task (clone configuration into a new task)
  {
    url: '/api/v1/tasks/:id/copy',
    method: 'post',
    response: ({ url, body }: { url: string; body: Record<string, unknown> }) => {
      const id = extractId(url)
      const source = mockTasks.find((t) => t.id === id) || mockTasks[0]
      const now = new Date().toISOString()
      const clone = {
        ...source,
        id: Math.max(...mockTasks.map((t) => t.id)) + 1,
        name: (body.name as string) || `${source.name} (copy)`,
        enabled: false,
        last_run: '',
        next_run: '',
        created_at: now,
        updated_at: now,
      }
      mockTasks.push(clone)
      return { code: 0, message: 'success', data: clone }
    },
  },
  // Execution stats for an optional time range
  {
    url: '/api/v1/tasks/stats',
    method: 'get',
    response: ({ query }: { query: Record<string, string> }) => {
      const fromTs = query?.from ? new Date(query.from).getTime() : 0
      const toTs = query?.to ? new Date(query.to).getTime() : 0
      const inRange = mockLogs.filter((l) => {
        const ts = new Date(l.started_at.replace(' ', 'T')).getTime()
        if (fromTs && ts < fromTs) return false
        if (toTs && ts > toTs) return false
        return true
      })
      const success = inRange.filter((l) => l.status === 'success').length
      const failed = inRange.filter((l) => l.status === 'failed').length
      const total = inRange.length
      const avgDuration = total
        ? Math.round(inRange.reduce((sum, l) => sum + (l.duration || 0), 0) / total)
        : 0
      return {
        code: 0,
        message: 'success',
        data: {
          total_executions: total,
          success_count: success,
          failure_count: failed,
          success_rate: total ? Math.round((success / total) * 10000) / 100 : 0,
          average_duration_ms: avgDuration,
        },
      }
    },
  },
  // Asset list is handled by telemetry mock (/api/v1/assets)
  // Execution log list (sub-resource: /tasks/:id/executions)
  {
    url: '/api/v1/tasks/:id/executions',
    method: 'get',
    response: ({ url, query }: { url: string; query: Record<string, string> }) => {
      const taskId = extractTaskId(url)
      const page = Number(query?.page) || 1
      const size = Number(query?.page_size) || 20
      let filtered = [...mockLogs]
      // taskId=0 means "all tasks"; otherwise filter by task_id
      if (taskId > 0) {
        filtered = filtered.filter((l) => l.task_id === taskId)
      }
      if (query?.task_name) {
        filtered = filtered.filter((l) => (l.task_name || '').includes(query.task_name))
      }
      if (query?.executor) {
        filtered = filtered.filter((l) => l.executor_type === query.executor)
      }
      if (query?.status) {
        filtered = filtered.filter((l) => l.status === query.status)
      }
      if (query?.from) {
        filtered = filtered.filter((l) => l.started_at >= query.from.replace('T', ' ').substring(0, 19))
      }
      if (query?.to) {
        filtered = filtered.filter((l) => l.started_at <= query.to.replace('T', ' ').substring(0, 19))
      }
      const total = filtered.length
      const start = (page - 1) * size
      const items = filtered.slice(start, start + size)
      return { code: 0, message: 'success', data: { items, total, page, page_size: size } }
    },
  },
  // Log detail (sub-resource: /tasks/:id/executions/:execId)
  {
    url: '/api/v1/tasks/:id/executions/:execId',
    method: 'get',
    response: ({ url }: { url: string }) => {
      const execId = extractExecId(url)
      const log = mockLogs.find((l) => l.id === execId)
      return { code: 0, message: 'success', data: log || mockLogs[0] }
    },
  },
  // Retry execution (sub-resource: /tasks/:id/executions/:execId/retry)
  {
    url: '/api/v1/tasks/:id/executions/:execId/retry',
    method: 'post',
    response: () => ({ code: 0, message: 'success', data: null }),
  },
] as MockMethod[]
