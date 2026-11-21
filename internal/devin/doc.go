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
package devin
