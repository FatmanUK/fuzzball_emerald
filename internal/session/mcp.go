package session

import "github.com/FatmanUK/fuzzball_emerald/internal/mcp"

// MCPPackages lists the out-of-band packages this server offers, with the
// version ranges from src/mcp.c.
//
// Most are advertised without a handler here: what they mean belongs to the
// game, which attaches its own handling. The negotiation package is the
// exception, because agreeing on what the other side supports is the
// protocol's own business.
func MCPPackages() []mcp.Package {
	one := mcp.Version{Major: 1, Minor: 0}
	two := mcp.Version{Major: 2, Minor: 0}
	return []mcp.Package{
		{Name: mcp.NegotiatePackage, MinVer: one, MaxVer: two,
			Handle: mcp.NegotiateHandler},
		{Name: "org-fuzzball-help", MinVer: one, MaxVer: one},
		{Name: "org-fuzzball-notify", MinVer: one, MaxVer: one},
		{Name: "org-fuzzball-simpleedit", MinVer: one, MaxVer: one},
		{Name: "org-fuzzball-languages", MinVer: one, MaxVer: one},
		{Name: "dns-org-mud-moo-simpleedit", MinVer: one, MaxVer: one},
		{Name: "org-fuzzball-gui", MinVer: one,
			MaxVer: mcp.Version{Major: 1, Minor: 3}},
	}
}
