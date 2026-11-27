// Package droid reads the subscription quota of Factory Droid from the login
// the droid CLI keeps on this computer.
//
// # Login
//
// Droid keeps its login in <home>/.factory as AES-256-GCM sealed JSON
//
//	{"access_token","refresh_token","active_organization_id"?,"whoami"?}
//
// The sealed text is base64(iv):base64(tag):base64(ciphertext) with a
// 16-byte IV and a 32-byte key stored as base64. Three files may hold it:
//
//   - auth.v2.loginkeychain, keyed by the generic password of service
//     "Factory CLI", account "auth-encryption-key-security-cli" in the macOS
//     login keychain, which droid writes with /usr/bin/security and which is
//     read the same way (the secret only travels on stdout);
//   - auth.v2.keyring, keyed by service "Factory CLI", account
//     "auth-encryption-key" of the system keyring: on Windows it reads the
//     Credential Manager generic credential "Factory CLI/auth-encryption-key"
//     (written by droid) first and falls back to
//     "Factory CLI:auth-encryption-key"; on Linux it reads the Secret Service
//     through go-keyring; on macOS it is skipped because that item belongs to
//     droid's own process and reading it would prompt;
//   - auth.v2.file, keyed by auth.v2.key in the same directory.
//
// Every readable file is opened and the login whose access token was issued
// last wins. The identity (email, name) comes from the access token's JWT
// claims without any network call. The package never writes, refreshes or
// removes the login: a refresh rotates the refresh token, which would sign
// droid out. An expired access token is ErrLoginExpired until droid renews
// it; the expired login is still returned for its identity.
//
// # Quota
//
// Fetch reads GET <origin>/api/billing/limits with the access token, where
// origin is https://api.factory.ai unless QUOSPARR_DEV_DROID_API_URL
// overrides it. The response carries the Standard and Core pools, each with
// a fiveHour, weekly and monthly window of usedPercent and windowEnd (null
// while the window has not started), and the extra usage balance in cents.
// hasActiveSubscription false without a Standard pool is ErrNoSubscription;
// any other response without a Standard pool is ErrBadResponse. The plan is
// organization.subscription.factoryTier of GET <origin>/api/app/auth/me; only
// that field is decoded and its failure leaves the plan empty without
// failing the reading. Requests send no cookies, never follow redirects and
// read at most 1 MiB. 401 and 403 are ErrUnauthorized, 429 ErrRateLimited.
//
// # Local Sessions
//
// SessionScanner reads <home>/.factory/sessions/<project>/<sessionId>.settings.json
// read-only (capped at 1 MiB per file; unreadable or malformed files are
// skipped). Each session carries tokenUsage (the session's own tokens, rather
// than inclusiveTokenUsage which would double-count sub-agent sessions that
// write their own settings.json). inputTokens is uncached input and
// thinkingTokens is already included in outputTokens. Models prefixed with
// "custom:" are BYOK and excluded from both token totals and API-equivalent
// cost. Because session .jsonl logs do not carry per-turn usage, a session's
// timestamp is approximated as max(mtime(.settings.json), mtime(.jsonl)).
// Droid reports only session-level totals without per-call prompt sizes, so
// models with context tiers are priced at their base Rate; Turns is 0.
//
// # Speed
//
// ScanSpeed reads each session's .settings.json within speed.Window before now
// (including "custom:" models). Each session with positive
// assistantActiveTimeMs and tokenUsage.outputTokens yields one session-level
// sample where TPS = outputTokens / assistantActiveTimeMs, which includes the
// wait for the first token and has no TTFT (HasTTFT is false). Reasoning effort
// comes from reasoningEffort; session title and project working directory come
// from the first "session_start" line of the sibling .jsonl log, falling back
// to the project directory name when cwd is absent. Results are sorted newest
// first and capped at speed.SessionLimit sessions.
//
// # Cache Hit
//
// ScanCache reads each session's .settings.json whose max mtime falls in
// [window.Start, window.End) (including "custom:" models). Each session with
// positive input = inputTokens + cacheReadTokens + cacheCreationTokens counts as
// one sample with cached = cacheReadTokens; unreadable or malformed files
// increment UnreadLines.
package droid
