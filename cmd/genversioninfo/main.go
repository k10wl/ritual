package main

import (
	"encoding/json"
	"fmt"
	"os"
	"ritual/internal/config"
)

func main() {
	out := "build/windows/info.json"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	version := fmt.Sprintf("%d.%d.%d", config.VersionMajor, config.VersionMinor, config.VersionPatch)
	data := map[string]any{
		"fixed": map[string]any{"file_version": version},
		"info": map[string]any{
			"0000": map[string]any{
				"ProductVersion":  version,
				"CompanyName":     config.GroupName,
				"FileDescription": config.Description,
				"LegalCopyright":  fmt.Sprintf("(c) %s", config.GroupName),
				"ProductName":     config.ProductName,
				"Comments":        "",
			},
		},
	}
	b, err := json.MarshalIndent(data, "", "\t")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
