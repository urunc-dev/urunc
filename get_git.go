package main

import (
	"fmt"
	"os/exec"
)

func main() {
	cmd := exec.Command("git", "show", "HEAD~1:pkg/containerd-shim/task_service.go")
	out, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
	}
	fmt.Println(string(out))
}
