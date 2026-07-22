package updater

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var versionRE = regexp.MustCompile(`^(?:v)?([0-9]+)\.([0-9]+)\.([0-9]+)$`)

type Version struct {
	Major int
	Minor int
	Patch int
}

func ParseVersion(value string) (Version, error) {
	match := versionRE.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return Version{}, fmt.Errorf("版本号 %q 不是 x.y.z 格式", value)
	}
	parts := [3]int{}
	for i := range parts {
		parsed, err := strconv.Atoi(match[i+1])
		if err != nil {
			return Version{}, err
		}
		parts[i] = parsed
	}
	return Version{Major: parts[0], Minor: parts[1], Patch: parts[2]}, nil
}

func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func Compare(a, b Version) int {
	left := [3]int{a.Major, a.Minor, a.Patch}
	right := [3]int{b.Major, b.Minor, b.Patch}
	for i := range left {
		if left[i] < right[i] {
			return -1
		}
		if left[i] > right[i] {
			return 1
		}
	}
	return 0
}
