package core

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	ProjectIDPrefix    = "prj_"
	RepositoryIDPrefix = "repo_"
	WorkspaceIDPrefix  = "ws_"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func ValidateID(value, prefix string) error {
	if !strings.HasPrefix(value, prefix) || !uuidPattern.MatchString(strings.TrimPrefix(value, prefix)) {
		return fmt.Errorf("invalid %s identifier", strings.TrimSuffix(prefix, "_"))
	}
	return nil
}

type RemoteIdentity struct {
	Host string `json:"host"`
	Port string `json:"port,omitempty"`
	Path string `json:"path"`
}

func (r RemoteIdentity) Validate() error {
	if r.Host == "" || r.Path == "" {
		return errors.New("remote identity requires host and path")
	}
	if r.Host != strings.ToLower(r.Host) || r.Host != strings.TrimSpace(r.Host) || strings.ContainsAny(r.Host, "/\\@\t\r\n ") {
		return errors.New("remote identity host is not normalized")
	}
	if r.Port != "" {
		port, err := strconv.Atoi(r.Port)
		if err != nil || port < 1 || port > 65535 || strconv.Itoa(port) != r.Port {
			return errors.New("remote identity port is invalid")
		}
	}
	if r.Path != strings.Trim(r.Path, "/") || strings.Contains(r.Path, "\\") || strings.Contains(r.Path, "//") || strings.HasSuffix(strings.ToLower(r.Path), ".git") || strings.ContainsAny(r.Path, "\t\r\n") {
		return errors.New("remote identity is not normalized")
	}
	for _, part := range strings.Split(r.Path, "/") {
		if part == "" || part == "." || part == ".." || strings.Contains(part, "@") {
			return errors.New("remote identity path is unsafe")
		}
	}
	return nil
}

func (r RemoteIdentity) Key() string {
	host := r.Host
	if r.Port != "" {
		host += ":" + r.Port
	}
	return host + "/" + r.Path
}

func (r RemoteIdentity) Equal(other RemoteIdentity) bool {
	return r.Host == other.Host && r.Port == other.Port && r.Path == other.Path
}

func NormalizeRemote(raw string) (RemoteIdentity, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RemoteIdentity{}, errors.New("remote URL is empty")
	}
	if strings.Contains(raw, "://") {
		return normalizeURLRemote(raw)
	}
	return normalizeSCPRemote(raw)
}

func normalizeURLRemote(raw string) (RemoteIdentity, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return RemoteIdentity{}, fmt.Errorf("parse remote URL: %w", err)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "ssh" {
		return RemoteIdentity{}, fmt.Errorf("unsupported remote transport %q", parsed.Scheme)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return RemoteIdentity{}, errors.New("remote URL query and fragment are not allowed")
	}
	host := strings.ToLower(parsed.Hostname())
	port := parsed.Port()
	if (scheme == "https" && port == "443") || (scheme == "ssh" && port == "22") {
		port = ""
	}
	pathValue, err := url.PathUnescape(parsed.EscapedPath())
	if err != nil {
		return RemoteIdentity{}, fmt.Errorf("decode remote path: %w", err)
	}
	return finishRemote(host, port, pathValue)
}

func normalizeSCPRemote(raw string) (RemoteIdentity, error) {
	if len(raw) >= 2 && raw[1] == ':' {
		return RemoteIdentity{}, errors.New("local filesystem paths are not remote identities")
	}
	colon := strings.IndexByte(raw, ':')
	if colon <= 0 || colon == len(raw)-1 {
		return RemoteIdentity{}, errors.New("invalid SCP-style remote")
	}
	hostPart := raw[:colon]
	if at := strings.LastIndexByte(hostPart, '@'); at >= 0 {
		hostPart = hostPart[at+1:]
	}
	if hostPart == "" || strings.ContainsAny(hostPart, "/\\") {
		return RemoteIdentity{}, errors.New("invalid SCP-style host")
	}
	return finishRemote(strings.ToLower(hostPart), "", raw[colon+1:])
}

func finishRemote(host, port, remotePath string) (RemoteIdentity, error) {
	remotePath = strings.ReplaceAll(remotePath, "\\", "/")
	remotePath = strings.Trim(remotePath, "/")
	for strings.HasSuffix(strings.ToLower(remotePath), ".git") {
		remotePath = remotePath[:len(remotePath)-4]
	}
	parts := strings.Split(remotePath, "/")
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		if part == "." || part == ".." || strings.Contains(part, "@") {
			return RemoteIdentity{}, errors.New("remote path contains an unsafe segment")
		}
		clean = append(clean, part)
	}
	identity := RemoteIdentity{Host: host, Port: port, Path: strings.Join(clean, "/")}
	if err := identity.Validate(); err != nil {
		return RemoteIdentity{}, err
	}
	return identity, nil
}
