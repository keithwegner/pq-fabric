# SOCKS5 boundary

This package implements a restricted private-testbed SOCKS5 CONNECT surface.

It is intentionally not a public exit proxy. CONNECT destinations must pass the
local exit allowlist used by the routing testbed. Unsupported commands are
rejected. The package exists to test local circuit integration and must not be
described as production anonymity, production privacy, censorship resistance, or
a public relay/exit system.
