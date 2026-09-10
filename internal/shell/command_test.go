package shell

import (
	"reflect"
	"testing"
)

func TestBareCommandKeepsParsedOutputConfigurationFree(t *testing.T) {
	cmd := Bare("printf result")
	if want := []string{"sh", "-c", "printf result"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
}

func TestHumanCommandReadsShellConfiguration(t *testing.T) {
	cmd := Human("/configured/shell", "configured-function")
	if cmd.Path != "/configured/shell" {
		t.Fatalf("path = %q, want /configured/shell", cmd.Path)
	}
	if want := []string{"/configured/shell", "-i", "-c", "configured-function"}; !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}
}
