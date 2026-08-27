# Security Policy

## Reporting a vulnerability

Do not disclose credential exposure or other security-sensitive details in a public issue.

Use GitHub's private vulnerability reporting feature from the repository's **Security** tab. If private reporting is unavailable, open a minimal issue requesting a private contact channel without including exploit instructions, cookies, tokens, signed media URLs, personal paths, or other sensitive data.

## Credentials and logs

YTGrab can process browser cookies and pasted Netscape cookie jars. Treat these as account credentials. Before sharing diagnostic output, remove cookies, authorization headers, local filesystem paths, IP addresses, and signed download URLs.

Security fixes are applied to the current `main` branch. Older builds are not maintained separately.
