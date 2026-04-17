package services

import (
	"net"
	"os/exec"
	"strings"
	"sync"
)

type SysInterfaceLister struct {
	once sync.Once
	name map[string]string
}

func NewSysInterfaceLister() *SysInterfaceLister {
	return &SysInterfaceLister{}
}

func (s *SysInterfaceLister) labelMap() map[string]string {
	s.once.Do(func() {
		out, err := exec.Command("networksetup", "-listallhardwareports").Output()
		if err != nil {
			s.name = map[string]string{}
			return
		}
		s.name = parseHardwarePorts(string(out))
	})
	return s.name
}

func (s *SysInterfaceLister) List() ([]Interface, error) {
	raw, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	labels := s.labelMap()
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
		label := r.Name
		if v, ok := labels[r.Name]; ok && v != "" {
			label = v
		}
		out = append(out, Interface{
			Name:  r.Name,
			Label: label,
			Up:    r.Flags&net.FlagUp != 0,
			Loop:  r.Flags&net.FlagLoopback != 0,
			IPs:   ips,
		})
	}
	return out, nil
}

func parseHardwarePorts(text string) map[string]string {
	res := map[string]string{}
	var port string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "VLAN Configurations" {
			break
		}
		if rest, ok := strings.CutPrefix(line, "Hardware Port:"); ok {
			port = strings.TrimSpace(rest)
			continue
		}
		if rest, ok := strings.CutPrefix(line, "Device:"); ok {
			dev := strings.TrimSpace(rest)
			if dev != "" && port != "" {
				res[dev] = port
			}
		}
	}
	return res
}
