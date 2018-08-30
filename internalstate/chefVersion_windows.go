package internalstate

import (
	"errors"
	"strings"

	"github.com/newvoicemedia/chef-waiter/cmd"
)

func chefVersion() (string, error) {
	stdout, _, exitCode := cmd.RunCommand("chef-client", "-v")
	if exitCode != 0 {
		return "", errors.New("Could not determin chef version")
	}
	version := strings.Split(stdout, " ")
	if len(version) > 1 {
		return cmd.Chomp(version[1]), nil
	}

	return cmd.Chomp(stdout), nil
}