// Package devin reads the Devin Desktop subscription quota with the login of
// the Devin CLI on this computer.
//
// # Login
//
// The CLI keeps its login in credentials.toml: $XDG_DATA_HOME/devin when
// XDG_DATA_HOME is set, else ~/.local/share/devin on macOS and Linux, and
// %APPDATA%\devin on Windows. The file is read, never written. Its
// windsurf_api_key is the credential and api_server_url the API origin
// (https://server.codeium.com when absent); an origin that is not https is
// refused so the key never travels in clear text. A login without
// windsurf_api_key (a Devin enterprise login billed in ACUs) has no
// subscription quota and is ErrUnsupportedLogin.
//
// # Quota
//
// Fetch posts the Connect JSON request
//
//	{"metadata":{"apiKey","ideName":"quosparr","ideVersion","extensionVersion","locale":"en"}}
//
// to <origin>/exa.seat_management_pb.SeatManagementService/GetUserStatus;
// QUOSPARR_DEV_DEVIN_API_URL overrides the origin. From userStatus it reads
// name and email, and from planStatus the planInfo planName,
// billingStrategy and hideDailyQuota, the daily and weekly remaining
// percentages and their reset times (Unix seconds, as JSON strings or
// numbers). Only billingStrategy BILLING_STRATEGY_QUOTA reports windows;
// proto3 JSON omits zero values, so a missing percentage of such a plan is
// 0 % remaining. Other plans give the plan name without windows. A Connect
// error "unauthenticated" or "permission_denied", or HTTP 401/403, is
// ErrUnauthorized. Requests send no cookies, never follow redirects and
// read at most 1 MiB.
//
// # Local Sessions
//
// SessionScanner reads <DataDir>/cli/sessions.db through a private temporary
// copy of the database and its -wal sidecar (via sqliteruntime.WithCopy),
// never opening or locking the live files written by the Devin CLI. It inspects
// assistant messages in message_nodes joined with sessions.working_directory,
// extracting metadata.generation_model, metadata.created_at (falling back to
// message_nodes.created_at), and metadata.metrics.{input_tokens,output_tokens,
// cache_read_tokens,cache_creation_tokens} where input_tokens is uncached input.
// Messages without metrics or with zero total tokens are skipped. Each
// counted assistant message increments Turns by 1 and selects any context tier
// using prompt = input + cache_read + cache_creation.
//
// # Speed
//
// ScanSpeed reads assistant messages within speed.Window before now from the
// same temporary SQLite copy, deduplicated by metadata.message_id (falling
// back to session_id and node_id when message_id is absent). Each message yields
// one speed.Sample using metadata.generation_model, timestamp
// metadata.started_generation_at (falling back to metadata.created_at and then
// message_nodes.created_at), and metadata.metrics.{ttft_ms,total_time_ms,
// output_tokens}. When ttft_ms is present, TTFT is ttft_ms and Decode is
// total_time_ms - ttft_ms; when ttft_ms is null, HasTTFT is false and Decode is
// total_time_ms. Samples are grouped by session with sessions.title, the base
// name of sessions.working_directory, and UpdatedAt as the latest sample or
// sessions.last_activity_at, keeping the newest speed.SessionLimit sessions.
//
// # Cache Hit
//
// ScanCache reads assistant messages in [window.Start, window.End) using the
// same timestamp and deduplication rules as ScanSpeed. When input_tokens,
// cache_read_tokens, and cache_creation_tokens are all non-null, input is their
// sum and cached is cache_read_tokens; when any of the three is null, the
// message increments MissingSamples across totals, model, and session rows.
package devin
