package source

import (
	"math"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/docker/docker/api/types"
	dockerimage "github.com/docker/docker/api/types/image"
	"gopkg.in/yaml.v3"

	"github.com/kraken/admin-tool/backend/internal/models"
)

// ParsedDockerfile represents the normalized AST-like structure of a Dockerfile
type ParsedDockerfile struct {
	BaseImages   []string
	Envs         map[string]string
	ExposedPorts []string
	Workdirs     []string
	Entrypoints  []string
	Cmds         []string
	RunCommands  []string
	Labels       map[string]string
}

// ParseDockerfile parses a Dockerfile string into normalized tokens
func ParseDockerfile(content string) ParsedDockerfile {
	parsed := ParsedDockerfile{
		Envs:   make(map[string]string),
		Labels: make(map[string]string),
	}

	lines := strings.Split(content, "\n")
	var currentInstruction string
	var currentBuffer strings.Builder

	// Merge continuation lines ending with \
	var joinedLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		if strings.HasSuffix(trimmed, "\\") {
			currentBuffer.WriteString(strings.TrimSuffix(trimmed, "\\"))
			currentBuffer.WriteString(" ")
		} else {
			currentBuffer.WriteString(trimmed)
			joinedLines = append(joinedLines, currentBuffer.String())
			currentBuffer.Reset()
		}
	}
	if currentBuffer.Len() > 0 {
		joinedLines = append(joinedLines, currentBuffer.String())
	}

	wsRegex := regexp.MustCompile(`\s+`)

	for _, line := range joinedLines {
		parts := wsRegex.Split(strings.TrimSpace(line), 2)
		if len(parts) == 0 {
			continue
		}
		instruction := strings.ToUpper(parts[0])
		body := ""
		if len(parts) > 1 {
			body = strings.TrimSpace(parts[1])
		}

		switch instruction {
		case "FROM":
			// Handle "FROM <image> as <alias>"
			fromParts := strings.Fields(body)
			if len(fromParts) > 0 {
				parsed.BaseImages = append(parsed.BaseImages, fromParts[0])
			}
		case "ENV":
			// Handle "ENV K=V" or "ENV K V"
			envPairs := parseEnvInstruction(body)
			for k, v := range envPairs {
				parsed.Envs[k] = v
			}
		case "EXPOSE":
			ports := strings.Fields(body)
			for _, p := range ports {
				parsed.ExposedPorts = append(parsed.ExposedPorts, strings.TrimSuffix(p, "/tcp"))
			}
		case "WORKDIR":
			parsed.Workdirs = append(parsed.Workdirs, body)
		case "ENTRYPOINT":
			parsed.Entrypoints = append(parsed.Entrypoints, cleanCmdArgs(body)...)
		case "CMD":
			parsed.Cmds = append(parsed.Cmds, cleanCmdArgs(body)...)
		case "RUN":
			parsed.RunCommands = append(parsed.RunCommands, body)
		case "LABEL":
			labelPairs := parseLabelInstruction(body)
			for k, v := range labelPairs {
				parsed.Labels[k] = v
			}
		}
		_ = currentInstruction
	}

	return parsed
}

func parseEnvInstruction(body string) map[string]string {
	res := make(map[string]string)
	if strings.Contains(body, "=") {
		tokens := strings.Fields(body)
		for _, tok := range tokens {
			kv := strings.SplitN(tok, "=", 2)
			if len(kv) == 2 {
				k := strings.TrimSpace(kv[0])
				v := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
				if k != "" {
					res[k] = v
				}
			}
		}
	} else {
		parts := strings.Fields(body)
		if len(parts) >= 2 {
			k := parts[0]
			v := strings.Trim(strings.Join(parts[1:], " "), `"'`)
			res[k] = v
		}
	}
	return res
}

func parseLabelInstruction(body string) map[string]string {
	res := make(map[string]string)
	tokens := strings.Fields(body)
	for _, tok := range tokens {
		kv := strings.SplitN(tok, "=", 2)
		if len(kv) == 2 {
			k := strings.TrimSpace(kv[0])
			v := strings.Trim(strings.TrimSpace(kv[1]), `"'`)
			if k != "" {
				res[k] = v
			}
		}
	}
	return res
}

func cleanCmdArgs(body string) []string {
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		trimmed = strings.Trim(trimmed, "[]")
		parts := strings.Split(trimmed, ",")
		var clean []string
		for _, p := range parts {
			c := strings.Trim(strings.TrimSpace(p), `"'`)
			if c != "" {
				clean = append(clean, c)
			}
		}
		return clean
	}
	return strings.Fields(trimmed)
}

// MatchImageWithCandidate scores the similarity between an image inspect/history and a candidate Dockerfile
func MatchImageWithCandidate(
	inspect types.ImageInspect,
	history []dockerimage.HistoryResponseItem,
	candidate CandidateFile,
) float64 {
	parsedCandidate := ParseDockerfile(candidate.Content)

	var score float64
	var maxScore float64

	// 1. Base Image Score (Weight 25)
	maxScore += 25.0
	baseMatched := false
	if len(parsedCandidate.BaseImages) > 0 {
		candBase := strings.ToLower(parsedCandidate.BaseImages[len(parsedCandidate.BaseImages)-1])
		// Check against history layers or inspect tags
		for _, h := range history {
			cb := strings.ToLower(h.CreatedBy)
			if strings.Contains(cb, candBase) {
				baseMatched = true
				break
			}
		}
		for _, tag := range inspect.RepoTags {
			if strings.HasPrefix(strings.ToLower(tag), candBase) {
				baseMatched = true
				break
			}
		}
	}
	if baseMatched {
		score += 25.0
	} else if len(parsedCandidate.BaseImages) == 0 {
		score += 10.0 // neutral
	}

	// 2. Environment Variables (Weight 20)
	maxScore += 20.0
	if len(parsedCandidate.Envs) > 0 {
		envMatched := 0
		inspectEnvMap := make(map[string]string)
		if inspect.Config != nil {
			for _, e := range inspect.Config.Env {
				kv := strings.SplitN(e, "=", 2)
				if len(kv) == 2 {
					inspectEnvMap[kv[0]] = kv[1]
				}
			}
		}
		for k, v := range parsedCandidate.Envs {
			if iv, exists := inspectEnvMap[k]; exists {
				if v == "" || v == iv {
					envMatched++
				} else {
					envMatched += 1
				}
			}
		}
		ratio := float64(envMatched) / float64(len(parsedCandidate.Envs))
		score += ratio * 20.0
	} else {
		score += 15.0 // neutral if candidate defines no specific ENV
	}

	// 3. Exposed Ports (Weight 15)
	maxScore += 15.0
	if len(parsedCandidate.ExposedPorts) > 0 {
		portMatched := 0
		inspectPorts := make(map[string]bool)
		if inspect.Config != nil && inspect.Config.ExposedPorts != nil {
			for p := range inspect.Config.ExposedPorts {
				inspectPorts[strings.TrimSuffix(string(p), "/tcp")] = true
			}
		}
		for _, p := range parsedCandidate.ExposedPorts {
			if inspectPorts[p] {
				portMatched++
			}
		}
		ratio := float64(portMatched) / float64(len(parsedCandidate.ExposedPorts))
		score += ratio * 15.0
	} else {
		score += 15.0
	}

	// 4. WorkingDir (Weight 10)
	maxScore += 10.0
	if len(parsedCandidate.Workdirs) > 0 && inspect.Config != nil {
		lastWorkdir := parsedCandidate.Workdirs[len(parsedCandidate.Workdirs)-1]
		if strings.TrimSuffix(lastWorkdir, "/") == strings.TrimSuffix(inspect.Config.WorkingDir, "/") {
			score += 10.0
		}
	} else {
		score += 8.0
	}

	// 5. Entrypoint & Cmd (Weight 15)
	maxScore += 15.0
	if inspect.Config != nil && (len(parsedCandidate.Entrypoints) > 0 || len(parsedCandidate.Cmds) > 0) {
		candTokens := append(parsedCandidate.Entrypoints, parsedCandidate.Cmds...)
		inspectTokens := append(inspect.Config.Entrypoint, inspect.Config.Cmd...)
		sim := tokenJaccardSimilarity(candTokens, inspectTokens)
		score += sim * 15.0
	} else {
		score += 10.0
	}

	// 6. RUN Commands layer similarity (Weight 15)
	maxScore += 15.0
	if len(parsedCandidate.RunCommands) > 0 && len(history) > 0 {
		var histCmds []string
		for _, h := range history {
			c := strings.TrimPrefix(h.CreatedBy, "/bin/sh -c ")
			c = strings.TrimPrefix(c, "#(nop) ")
			histCmds = append(histCmds, c)
		}
		sim := tokenJaccardSimilarity(
			tokenizeCommands(parsedCandidate.RunCommands),
			tokenizeCommands(histCmds),
		)
		score += sim * 15.0
	} else {
		score += 10.0
	}

	finalScore := score / maxScore

	// Path and label hints bonus (up to +0.10)
	candDir := strings.ToLower(filepath.Dir(candidate.Path))
	candBase := strings.ToLower(filepath.Base(candidate.Path))

	// Bonus if image label working_dir matches candidate path
	if inspect.Config != nil && inspect.Config.Labels != nil {
		if workingDirLabel := inspect.Config.Labels["com.docker.compose.project.working_dir"]; workingDirLabel != "" {
			if strings.EqualFold(filepath.Clean(workingDirLabel), filepath.Clean(filepath.Dir(candidate.Path))) {
				finalScore += 0.15
			}
		}
	}

	for _, tag := range inspect.RepoTags {
		cleanTag := strings.ToLower(tag)
		repoName := strings.Split(cleanTag, ":")[0]
		shortName := filepath.Base(repoName)
		if strings.Contains(candDir, shortName) || strings.Contains(candBase, shortName) {
			finalScore += 0.05
			break
		}
	}

	return math.Min(1.0, math.Max(0.0, finalScore))
}

func tokenizeCommands(cmds []string) []string {
	var tokens []string
	for _, cmd := range cmds {
		words := strings.Fields(cmd)
		for _, w := range words {
			clean := strings.Trim(w, `&|;\"'`)
			if len(clean) > 2 {
				tokens = append(tokens, strings.ToLower(clean))
			}
		}
	}
	return tokens
}

func tokenJaccardSimilarity(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	if len(a) == 0 || len(b) == 0 {
		return 0.0
	}

	setA := make(map[string]bool)
	for _, item := range a {
		setA[strings.ToLower(item)] = true
	}
	setB := make(map[string]bool)
	for _, item := range b {
		setB[strings.ToLower(item)] = true
	}

	intersection := 0
	for k := range setA {
		if setB[k] {
			intersection++
		}
	}

	union := len(setA) + len(setB) - intersection
	if union == 0 {
		return 0.0
	}
	return float64(intersection) / float64(union)
}

// ParsedCompose represents parsed service and image information from a Compose file
type ParsedCompose struct {
	Version  string
	Services map[string]ComposeServiceInfo
}

type ComposeServiceInfo struct {
	Image       string
	Build       string
	Ports       []string
	Environment map[string]string
}

// ParseComposeYAML parses a docker-compose.yml file content
func ParseComposeYAML(content string) (ParsedCompose, error) {
	var raw struct {
		Version  string                 `yaml:"version"`
		Services map[string]yaml.Node   `yaml:"services"`
	}

	var parsed ParsedCompose
	parsed.Services = make(map[string]ComposeServiceInfo)

	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return parsed, err
	}
	parsed.Version = raw.Version

	for sName, node := range raw.Services {
		var sInfo struct {
			Image       string      `yaml:"image"`
			Build       interface{} `yaml:"build"`
			Ports       []string    `yaml:"ports"`
			Environment interface{} `yaml:"environment"`
		}
		if err := node.Decode(&sInfo); err == nil {
			info := ComposeServiceInfo{
				Image: sInfo.Image,
				Ports: sInfo.Ports,
			}
			if bStr, ok := sInfo.Build.(string); ok {
				info.Build = bStr
			}
			parsed.Services[sName] = info
		}
	}

	return parsed, nil
}

// MatchStackWithCandidate scores the similarity between a running Compose stack and a candidate compose file
func MatchStackWithCandidate(stack models.ComposeStack, candidate CandidateFile) float64 {
	cleanCandidatePath := filepath.Clean(candidate.Path)

	// 1. Exact config file path match -> 1.0
	if stack.ConfigFiles != "" {
		for _, cfg := range strings.Split(stack.ConfigFiles, ",") {
			if filepath.Clean(strings.TrimSpace(cfg)) == cleanCandidatePath {
				return 1.0
			}
		}
	}

	// 2. Inside working dir match
	if stack.WorkingDir != "" && strings.HasPrefix(cleanCandidatePath, filepath.Clean(stack.WorkingDir)) {
		base := strings.ToLower(filepath.Base(candidate.Path))
		if strings.HasPrefix(base, "docker-compose") || strings.HasPrefix(base, "compose") {
			return 0.98
		}
	}

	parsed, err := ParseComposeYAML(candidate.Content)
	if err != nil || len(parsed.Services) == 0 {
		return 0.0
	}

	// 3. Service name overlap
	stackServices := make(map[string]models.ComposeService)
	for _, s := range stack.Services {
		stackServices[strings.ToLower(s.Name)] = s
	}

	serviceMatches := 0
	imageMatches := 0

	for sName, cService := range parsed.Services {
		if sMatch, exists := stackServices[strings.ToLower(sName)]; exists {
			serviceMatches++
			if cService.Image != "" && sMatch.Image != "" {
				cImg := strings.ToLower(cService.Image)
				sImg := strings.ToLower(sMatch.Image)
				if cImg == sImg || strings.Contains(sImg, cImg) || strings.Contains(cImg, sImg) {
					imageMatches++
				}
			}
		}
	}

	serviceRatio := float64(serviceMatches) / float64(len(parsed.Services))
	imageRatio := 0.0
	if serviceMatches > 0 {
		imageRatio = float64(imageMatches) / float64(serviceMatches)
	}

	score := (serviceRatio * 0.6) + (imageRatio * 0.4)

	// Directory name matching stack name bonus
	candDir := strings.ToLower(filepath.Base(filepath.Dir(candidate.Path)))
	if strings.EqualFold(candDir, stack.Name) {
		score += 0.2
	}

	return math.Min(1.0, math.Max(0.0, score))
}
