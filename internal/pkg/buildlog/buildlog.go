package buildlog

import (
	"bufio"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

type BuildLog struct {
	Name    string
	Project string
	Distro  string
	Arch    string
	rawlog  string
	Phases  []BuildPhaseDetails
}

// BuildPhase defines the different phases of a build process.
type BuildPhase int

// The different phases of a build process.
const (
	Header BuildPhase = iota
	Preinstall
	CopyingPackages
	VMBoot
	PackageCumulation
	PackageInstallation
	Build
	PostBuildChecks
	RPMLintReport
	PackageComparison
	Summary
	Retries
)

var buildPhaseNames = [...]string{
	"Header",
	"Preinstall",
	"CopyingPackages",
	"VMBoot",
	"PackageCumulation",
	"PackageInstallation",
	"Build",
	"PostBuildChecks",
	"RPMLintReport",
	"PackageComparison",
	"Summary",
	"Retries",
}

type BuildPhaseDetails struct {
	Lines      []string       `json:"lines,omitempty"`
	Success    bool           `json:"success"`
	Duration   int            `json:"duration_seconds,omitempty"`
	Properties map[string]any `json:"properties,omitempty"`
	Phase      BuildPhase     `json:"phase"`
}

// String returns the string representation of the build phase.
func (p BuildPhase) String() string {
	if p < Header || p > Retries {
		return "Unknown"
	}
	return buildPhaseNames[p]
}

// MarshalJSON implements the json.Marshaler interface.
func (p BuildPhase) MarshalJSON() ([]byte, error) {
	return json.Marshal(p.String())
}

type VM_Type int

const (
	kvm VM_Type = iota
	chroot
)

var vm_type_name = [...]string{
	"kvm",
	"chroot",
}

// extractTime extracts the time in seconds from a log line.
// It returns the time and true if successful, otherwise 0 and false.
func extractTime(line string) (int, bool) {
	re := regexp.MustCompile(`^\[\s*(\d+)s\]`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 2 {
		return 0, false
	}
	seconds, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0, false
	}
	return seconds, true
}

func ParseLog(logContent string) (*BuildLog, error) {
	log := &BuildLog{
		rawlog: logContent,
		Phases: make([]BuildPhaseDetails, 0),
	}
	scanner := bufio.NewScanner(strings.NewReader(logContent))
	phase := Header
	var currentPhaseDetails BuildPhaseDetails
	var phaseStartTime int
	var lastTime int

	buildInfoRegex := regexp.MustCompile(`Building (\S+) for project '(\S+)' repository '(\S+)' arch '(\S+)'`)

	for scanner.Scan() {
		line := scanner.Text()

		if newTime, ok := extractTime(line); ok {
			lastTime = newTime
		}

		if matches := buildInfoRegex.FindStringSubmatch(line); len(matches) == 5 {
			log.Name = matches[1]
			log.Project = matches[2]
			log.Distro = matches[3]
			log.Arch = matches[4]
		}

		newPhase := nextPhase(phase, line)

		if newPhase != phase {
			currentPhaseDetails.Phase = phase
			currentPhaseDetails.Duration = lastTime - phaseStartTime
			log.Phases = append(log.Phases, currentPhaseDetails)
			currentPhaseDetails = BuildPhaseDetails{}
			phase = newPhase
			phaseStartTime = lastTime
		}
		currentPhaseDetails.Lines = append(currentPhaseDetails.Lines, line)
	}
	currentPhaseDetails.Phase = phase
	currentPhaseDetails.Duration = lastTime - phaseStartTime
	log.Phases = append(log.Phases, currentPhaseDetails)

	return log, nil
}

var phaseMatches = []struct {
	phase   BuildPhase
	matcher *regexp.Regexp
}{
	{Header, regexp.MustCompile(`^\[`)},
	{Preinstall, regexp.MustCompile(`^\[\s*\d+s\] \[[\s\d/]+\] preinstalling`)},
	{CopyingPackages, regexp.MustCompile(`^\[\s*\d+s\] copying packages\.`)},
	{VMBoot, regexp.MustCompile(`^\[\s*\d+s\] booting kvm\.`)},
	{PackageCumulation, regexp.MustCompile(`^\[\s*\d+s\] \[[\s\d/]+\] cumulate`)},
	{PackageInstallation, regexp.MustCompile(`^\[\s*\d+s\] now installing cumulated packages`)},
	{Build, regexp.MustCompile(`^\[\s*\d+s\] -----------------------------------------------------------------`)},
	{PostBuildChecks, regexp.MustCompile(`^\[\s*\d+s\] \.\.\. checking for files with abuild user/group`)},
	{RPMLintReport, regexp.MustCompile(`^\[\s*\d+s\] RPMLINT report:`)},
	{PackageComparison, regexp.MustCompile(`^\[\s*\d+s\] \.\.\. comparing built packages with the former built`)},
	{Summary, regexp.MustCompile(`^\[\s*\d+s\] i\d+-.+ finished "build .+"`)},
	{Retries, regexp.MustCompile(`^Retried build at`)},
}

func nextPhase(current BuildPhase, line string) BuildPhase {
	for i := int(current) + 1; i < len(phaseMatches); i++ {
		if phaseMatches[i].matcher.MatchString(line) {
			return phaseMatches[i].phase
		}
	}
	return current
}

func (log *BuildLog) FormatJson() map[string]any {
	result := make(map[string]any)

	properties := make(map[string]any)
	properties["Name"] = log.Name
	properties["Project"] = log.Project
	properties["Distro"] = log.Distro
	properties["Arch"] = log.Arch
	result["Properties"] = properties

	phasesMap := make(map[string]any)
	for _, phaseDetail := range log.Phases {
		phaseData := make(map[string]any)
		phaseData["lines"] = phaseDetail.Lines
		phaseData["success"] = phaseDetail.Success
		if phaseDetail.Duration != 0 {
			phaseData["duration_seconds"] = phaseDetail.Duration
		}
		if len(phaseDetail.Properties) > 0 {
			phaseData["properties"] = phaseDetail.Properties
		}
		phasesMap[phaseDetail.Phase.String()] = phaseData
	}
	result["Phases"] = phasesMap

	return result
}
