package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type instruction struct {
	Op   string `json:"op"`
	Path string `json:"path"`
	Data string `json:"data"`
	Code int    `json:"code"`
}

func main() {
	root := flag.String("root", ".", "working directory for file operations")
	flag.Parse()

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		var inst instruction
		if err := json.Unmarshal(line, &inst); err != nil {
			if handleConsoleCommand(strings.TrimSpace(string(line))) {
				return
			}
			continue
		}
		if err := execute(*root, inst); err != nil {
			fmt.Fprintf(os.Stderr, "execute %s: %s\n", inst.Op, err)
			os.Exit(2)
		}
		if inst.Op == "exit" {
			os.Exit(inst.Code)
		}
	}
	if err := scanner.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "stdin read: %s\n", err)
		os.Exit(2)
	}
}

func handleConsoleCommand(cmd string) bool {
	switch cmd {
	case "save-off":
		fmt.Println("[Server thread/INFO]: Automatic saving is now disabled")
		return false
	case "save-on":
		fmt.Println("[Server thread/INFO]: Automatic saving is now enabled")
		return false
	case "save-all flush", "save-all":
		fmt.Println("[Server thread/INFO]: Saving the game (this may take a moment!)")
		fmt.Println("[Server thread/INFO]: Saved the game")
		return false
	case "stop":
		fmt.Println("[Server thread/INFO]: Stopping the server")
		return true
	default:
		return false
	}
}

func execute(root string, inst instruction) error {
	switch inst.Op {
	case "write":
		return writeFile(root, inst)
	case "delete":
		return deleteFile(root, inst)
	case "exit":
		return nil
	default:
		return fmt.Errorf("unknown op: %s", inst.Op)
	}
}

func writeFile(root string, inst instruction) error {
	fullPath := filepath.Join(root, filepath.FromSlash(inst.Path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}
	data, err := base64.StdEncoding.DecodeString(inst.Data)
	if err != nil {
		return fmt.Errorf("decode base64: %w", err)
	}
	return os.WriteFile(fullPath, data, 0644)
}

func deleteFile(root string, inst instruction) error {
	fullPath := filepath.Join(root, filepath.FromSlash(inst.Path))
	err := os.Remove(fullPath)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
