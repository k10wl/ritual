package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"ritual/internal/config"
)

func main() {
	// -dev suffixes ProductName with " Dev" so the .syso resource (visible in
	// Windows Properties, Task Manager, etc.) telegraphs the variant. The flag
	// rather than reading config.AppName because genversioninfo runs via
	// `go run`, which never picks up the build's -X ldflag override.
	dev := flag.Bool("dev", false, "produce dev-variant resource info (suffix ProductName with \" Dev\")")
	flag.Parse()
	out := "build/windows/info.json"
	if args := flag.Args(); len(args) > 0 {
		out = args[0]
	}
	productName := config.ProductName
	if *dev {
		productName += " Dev"
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
				"ProductName":     productName,
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
