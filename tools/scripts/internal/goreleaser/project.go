package goreleaser

const (
	mainPath   = "./cmd/ageAPIServer"
	binaryName = "ageAPIServer"
)

// documentFiles ship with every archive. The configuration is included because
// it is the file the binary reads by default, and it is where the listener's
// TLS mode is documented.
var documentFiles = []string{"README.md", "api_spec.yml", "resources/config/config.toml"}

func newConfig(legacyGo string) Config {
	return Config{
		Version:     2,
		ProjectName: "ageAPIServer",
		Builds: []Build{
			build("ageAPIServer-win7-386", "windows", "386", legacyGo, nil, nil),
			build("ageAPIServer-win7-amd64", "windows", "amd64", legacyGo, nil, nil),
			build("ageAPIServer-win10-386", "windows", "386", "", nil, nil),
			build("ageAPIServer-win10-amd64", "windows", "amd64", "", nil, nil),
			build("ageAPIServer-win10-arm64", "windows", "arm64", "", nil, nil),
			build("ageAPIServer-linux-386", "linux", "386", "", nil, nil),
			build("ageAPIServer-linux-amd64", "linux", "amd64", "", nil, nil),
			build("ageAPIServer-linux-arm64", "linux", "arm64", "", nil, nil),
			build("ageAPIServer-linux-arm", "linux", "arm", "", []string{"5", "6"}, nil),
			build("ageAPIServer-darwin-amd64", "darwin", "amd64", "", nil, nil),
			build("ageAPIServer-darwin-arm64", "darwin", "arm64", "", nil, []string{"v8.0"}),
		},
		Archives: []Archive{
			archive("ageAPIServer-win7", []string{"ageAPIServer-win7-386", "ageAPIServer-win7-amd64"}, "zip", "{{ .ProjectName }}_{{ .RawVersion }}_win7_{{ if eq .Arch \"386\" }}x86-32{{ else if eq .Arch \"amd64\" }}x86-64{{ else }}{{ .Arch }}{{ end }}"),
			archive("ageAPIServer-win10", []string{"ageAPIServer-win10-386", "ageAPIServer-win10-amd64", "ageAPIServer-win10-arm64"}, "zip", "{{ .ProjectName }}_{{ .RawVersion }}_win10_{{ if eq .Arch \"386\" }}x86-32{{ else if eq .Arch \"amd64\" }}x86-64{{ else }}{{ .Arch }}{{ end }}"),
			archive("ageAPIServer-linux-386", []string{"ageAPIServer-linux-386"}, "tar.gz", "{{ .ProjectName }}_{{ .RawVersion }}_linux_x86-32"),
			archive("ageAPIServer-linux-amd64", []string{"ageAPIServer-linux-amd64"}, "tar.gz", "{{ .ProjectName }}_{{ .RawVersion }}_linux_x86-64"),
			archive("ageAPIServer-linux-arm64", []string{"ageAPIServer-linux-arm64"}, "tar.gz", "{{ .ProjectName }}_{{ .RawVersion }}_linux_arm64"),
			archive("ageAPIServer-linux-arm", []string{"ageAPIServer-linux-arm"}, "tar.gz", "{{ .ProjectName }}_{{ .RawVersion }}_linux_arm-{{ .Arm }}"),
			archive("ageAPIServer-darwin-universal", []string{"ageAPIServer-darwin-universal"}, "tar.gz", "{{ .ProjectName }}_{{ .RawVersion }}_mac"),
		},
		UniversalBinaries: []UniversalBinary{{
			ID: "ageAPIServer-darwin-universal", IDs: []string{"ageAPIServer-darwin-amd64", "ageAPIServer-darwin-arm64"}, NameTemplate: binaryName,
		}},
		Checksum:  Checksum{NameTemplate: "{{ .ProjectName }}_{{ .RawVersion }}_checksums.txt"},
		Release:   Release{Draft: true, Prerelease: "auto"},
		Changelog: Changelog{Sort: "asc", Filters: Filters{Exclude: []string{"^docs:", "^test:"}}},
		Signs:     []Sign{{Artifacts: "checksum", Cmd: "gpg", Args: []string{"--batch", "--pinentry-mode", "loopback", "--passphrase", "{{ .Env.GPG_PASSPHRASE }}", "--local-user", "{{ .Env.GPG_FINGERPRINT }}", "--output", "${signature}", "--detach-sign", "${artifact}"}}},
	}
}

func build(id, goos, goarch, tool string, goarm, goarm64 []string) Build {
	return Build{
		ID:      id,
		Main:    mainPath,
		Binary:  binaryName,
		Goos:    []string{goos},
		Goarch:  []string{goarch},
		Tool:    tool,
		Goarm:   goarm,
		Goarm64: goarm64,
		Flags:   []string{"-trimpath"},
		Ldflags: []string{"-s", "-w", "-X", "main.version={{ .Version }}"},
	}
}

func archive(id string, ids []string, format, name string) Archive {
	return Archive{ID: id, IDs: ids, Formats: []string{format}, NameTemplate: name, Files: documentFiles}
}
