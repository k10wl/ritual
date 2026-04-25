package netinfo

import "net"

// SysInterfaceLister is the default InterfaceLister backed by net.Interfaces.
// The composition root (cmd/gui) wires it into AddressProvider; tests stub
// InterfaceLister directly instead of exercising real hardware.
type SysInterfaceLister struct{}

// NewSysInterfaceLister builds a stateless lister.
func NewSysInterfaceLister() *SysInterfaceLister {
	return &SysInterfaceLister{}
}

// List enumerates host network interfaces.
func (SysInterfaceLister) List() ([]Interface, error) {
	raw, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make([]Interface, 0, len(raw))
	for _, r := range raw {
		addrs, err := r.Addrs()
		if err != nil {
			continue
		}
		ips := make([]net.IP, 0, len(addrs))
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ips = append(ips, ipn.IP)
		}
		out = append(out, Interface{
			Name:  r.Name,
			Label: r.Name,
			Up:    r.Flags&net.FlagUp != 0,
			Loop:  r.Flags&net.FlagLoopback != 0,
			IPs:   ips,
		})
	}
	return out, nil
}
