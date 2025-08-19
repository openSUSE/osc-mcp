package buildlog

import (
	"bufio"
	"regexp"
	"strconv"
	"strings"
)

type BuildPhase int

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
	Unknown
)

func (p BuildPhase) String() string {
	return [...]string{
		"Header",
		"Preinstall",
		"Copying packages",
		"VM boot",
		"Package cumulation",
		"Package installation",
		"Build",
		"Post build checks",
		"RPM lint report",
		"Package comparison",
		"Summary",
		"Retries",
		"Unknown",
	}[p]
}

type Phase struct {
	Lines    []string
	Duration int
}

type BuildLog struct {
	Name    string
	Project string
	Distro  string
	Arch    string
	Phases  map[BuildPhase]Phase
	rawlog  string
}

var (
	buildInfoRegex  = regexp.MustCompile(`Building (\S+) for project '([^']+)' repository '([^']+)' arch '([^']+)'`)
	localBuildRegex = regexp.MustCompile(`started "build (\S+)\.spec"`)
	localBuildRoot  = regexp.MustCompile(`Using BUILD_ROOT=.*/([^-]+)-([^-/]+)`)
)

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
	{Summary, regexp.MustCompile(`^\[\s*\d+s\] \S+ finished "build .+"`)},
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

func Parse(logContent string) *BuildLog {
	log := &BuildLog{
		Phases: make(map[BuildPhase]Phase),
		rawlog: logContent,
	}
	scanner := bufio.NewScanner(strings.NewReader(logContent))
	phase := Header
	var currentPhaseDetails Phase
	var phaseStartTime int
	var lastTime int

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
		} else if matches := localBuildRegex.FindStringSubmatch(line); len(matches) == 2 {
			log.Name = matches[1]
		} else if matches := localBuildRoot.FindStringSubmatch(line); len(matches) == 3 {
			log.Distro = matches[1]
			log.Arch = matches[2]
			log.Project = "local"
		}

		newPhase := nextPhase(phase, line)

		if newPhase != phase {
			currentPhaseDetails.Duration = lastTime - phaseStartTime
			log.Phases[phase] = currentPhaseDetails
			currentPhaseDetails = Phase{}
			phase = newPhase
			phaseStartTime = lastTime
		}
		currentPhaseDetails.Lines = append(currentPhaseDetails.Lines, line)
	}
	currentPhaseDetails.Duration = lastTime - phaseStartTime
	log.Phases[phase] = currentPhaseDetails

	return log
}

func (log *BuildLog) FormatJson() map[string]any {
	properties := map[string]string{
		"Name":    log.Name,
		"Project": log.Project,
		"Distro":  log.Distro,
		"Arch":    log.Arch,
	}

	phases := make(map[string]any)
	for phase, phaseDetails := range log.Phases {
		phaseSuccess := true
		for _, line := range phaseDetails.Lines {
			// A simple heuristic to detect failures.
			// This can be improved with more sophisticated checks.
			if strings.Contains(line, " FAILED") || strings.Contains(line, " ERROR") {
				phaseSuccess = false
				break
			}
		}

		phases[phase.String()] = map[string]any{
			"Lines":    phaseDetails.Lines,
			"Duration": phaseDetails.Duration,
			"Success":  phaseSuccess,
		}
	}

	return map[string]any{
		"Properties": properties,
		"Phases":     phases,
	}
}
