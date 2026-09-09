package deps

import (
	"bufio"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// CheckoutHolder is a process that can keep a checkout on disk after removal.
type CheckoutHolder struct {
	PID  int
	Name string
}

// CheckoutHolderProbe finds processes working in a checkout and Git's file
// monitor daemon when its socket is still in the checkout administration.
type CheckoutHolderProbe interface {
	CheckoutHolders(checkoutPath, gitDir string) ([]CheckoutHolder, error)
}

// RealCheckoutHolderProbe asks lsof for the process information the operating
// system exposes. An unavailable lsof is reported to the caller, which can
// continue removal because this is advisory information.
type RealCheckoutHolderProbe struct {
	run func(args ...string) (string, error)
}

func NewRealCheckoutHolderProbe() *RealCheckoutHolderProbe {
	return &RealCheckoutHolderProbe{run: func(args ...string) (string, error) {
		out, err := exec.Command("lsof", args...).Output()
		return string(out), err
	}}
}

func (p *RealCheckoutHolderProbe) CheckoutHolders(checkoutPath, gitDir string) ([]CheckoutHolder, error) {
	cwdOutput, err := p.run("-n", "-Fpcn", "-a", "-d", "cwd")
	if err != nil {
		return nil, err
	}
	holders := holdersInDirectory(cwdOutput, checkoutPath)

	if gitDir != "" {
		socketOutput, err := p.run("-n", "-Fpc", filepath.Join(gitDir, "fsmonitor--daemon.ipc"))
		if err == nil {
			holders = append(holders, holdersFromLsof(socketOutput)...)
		}
	}
	return uniqueCheckoutHolders(holders), nil
}

func holdersInDirectory(output, checkoutPath string) []CheckoutHolder {
	checkoutPath = filepath.Clean(checkoutPath)
	var holders []CheckoutHolder
	for _, record := range lsofRecords(output) {
		if pathWithin(record.path, checkoutPath) {
			holders = append(holders, record.holder)
		}
	}
	return holders
}

func holdersFromLsof(output string) []CheckoutHolder {
	var holders []CheckoutHolder
	var current CheckoutHolder
	for scanner := bufio.NewScanner(strings.NewReader(output)); scanner.Scan(); {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			if current.PID != 0 {
				holders = append(holders, current)
			}
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				current = CheckoutHolder{PID: pid}
			}
		case 'c':
			current.Name = line[1:]
		}
	}
	if current.PID != 0 {
		holders = append(holders, current)
	}
	return holders
}

type lsofRecord struct {
	holder CheckoutHolder
	path   string
}

func lsofRecords(output string) []lsofRecord {
	var records []lsofRecord
	var current CheckoutHolder
	for scanner := bufio.NewScanner(strings.NewReader(output)); scanner.Scan(); {
		line := scanner.Text()
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, err := strconv.Atoi(line[1:])
			if err == nil {
				current = CheckoutHolder{PID: pid}
			}
		case 'c':
			current.Name = line[1:]
		case 'n':
			if current.PID != 0 {
				records = append(records, lsofRecord{holder: current, path: line[1:]})
			}
		}
	}
	return records
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func uniqueCheckoutHolders(holders []CheckoutHolder) []CheckoutHolder {
	seen := make(map[CheckoutHolder]bool, len(holders))
	unique := make([]CheckoutHolder, 0, len(holders))
	for _, holder := range holders {
		if holder.PID == 0 || seen[holder] {
			continue
		}
		seen[holder] = true
		unique = append(unique, holder)
	}
	sort.Slice(unique, func(i, j int) bool {
		if unique[i].PID != unique[j].PID {
			return unique[i].PID < unique[j].PID
		}
		return unique[i].Name < unique[j].Name
	})
	return unique
}
