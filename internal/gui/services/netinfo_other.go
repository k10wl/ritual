//go:build !darwin

package services

import "net"

type SysInterfaceLister struct{}

func NewSysInterfaceLister() *SysInterfaceLister {
	return &SysInterfaceLister{}
}

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
