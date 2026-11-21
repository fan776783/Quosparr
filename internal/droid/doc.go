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
//     "auth-encryption-key" of the system keyring, read through go-keyring on
//     Windows and Linux only: on macOS that item belongs to droid's own
//     process and reading it would prompt;
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
package droid
