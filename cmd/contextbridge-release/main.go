package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/ukyovfx/Context-Bridge/internal/release"
)

func main() {
	version := flag.String("version", "", "release version")
	commit := flag.String("commit", "", "source Git commit")
	goVersion := flag.String("go-version", runtime.Version(), "Go version")
	windowsBinary := flag.String("windows-binary", "", "Windows executable")
	linuxBinary := flag.String("linux-binary", "", "Linux executable")
	installer := flag.String("installer", "", "optional Windows installer")
	output := flag.String("output", "", "release output directory")
	flag.Parse()
	if *version == "" || *commit == "" || *windowsBinary == "" || *linuxBinary == "" || *output == "" {
		fatal("version, commit, both binaries, and output are required")
	}
	windowsName, linuxName, err := release.ArtifactNames(*version)
	if err != nil {
		fatal(err.Error())
	}
	if err := os.MkdirAll(*output, 0o755); err != nil {
		fatal(err.Error())
	}
	readme := []byte("Context Bridge\n\nPortable installation:\n1. Extract this archive.\n2. Add the extracted directory to your user PATH.\n3. Open a new terminal and run: contextbridge setup\n\nUser state remains separate from the installed binary.\n")
	if err := writeZip(filepath.Join(*output, windowsName), map[string]string{"contextbridge.exe": *windowsBinary, "LICENSE": "LICENSE", "README.txt": ""}, readme); err != nil {
		fatal(err.Error())
	}
	if err := writeTarGz(filepath.Join(*output, linuxName), map[string]string{"contextbridge": *linuxBinary, "LICENSE": "LICENSE", "README.txt": ""}, readme); err != nil {
		fatal(err.Error())
	}
	artifactFiles := []string{filepath.Join(*output, windowsName), filepath.Join(*output, linuxName)}
	checksumNames := []string{windowsName, linuxName}
	if *installer != "" {
		installerName := filepath.Base(*installer)
		destination := filepath.Join(*output, installerName)
		if err := copyFile(*installer, destination); err != nil {
			fatal(err.Error())
		}
		artifactFiles = append(artifactFiles, destination)
		checksumNames = append(checksumNames, installerName)
	}
	manifest, err := release.BuildManifest(*version, *commit, *goVersion, artifactFiles)
	if err != nil {
		fatal(err.Error())
	}
	if err := release.WriteManifest(filepath.Join(*output, "release-manifest.json"), manifest); err != nil {
		fatal(err.Error())
	}
	if err := writeChecksums(*output, checksumNames); err != nil {
		fatal(err.Error())
	}
}

func copyFile(source, destination string) error {
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, data, 0o755)
}

func writeZip(path string, files map[string]string, readme []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	archive := zip.NewWriter(file)
	names := sortedKeys(files)
	for _, name := range names {
		data := readArchiveData(files[name], name, readme)
		header := &zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Unix(0, 0).UTC()}
		writer, err := archive.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
	}
	return archive.Close()
}

func writeTarGz(path string, files map[string]string, readme []byte) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed := gzip.NewWriter(file)
	archive := tar.NewWriter(compressed)
	for _, name := range sortedKeys(files) {
		data := readArchiveData(files[name], name, readme)
		header := &tar.Header{Name: name, Mode: 0o644, Size: int64(len(data)), ModTime: time.Unix(0, 0).UTC(), AccessTime: time.Unix(0, 0).UTC(), ChangeTime: time.Unix(0, 0).UTC()}
		if name == "contextbridge" {
			header.Mode = 0o755
		}
		if err := archive.WriteHeader(header); err != nil {
			return err
		}
		if _, err := archive.Write(data); err != nil {
			return err
		}
	}
	if err := archive.Close(); err != nil {
		return err
	}
	return compressed.Close()
}

func readArchiveData(source, name string, readme []byte) []byte {
	if source == "" && name == "README.txt" {
		return readme
	}
	data, err := os.ReadFile(source)
	if err != nil {
		fatal(err.Error())
	}
	return data
}

func writeChecksums(output string, names []string) error {
	sort.Strings(names)
	file, err := os.Create(filepath.Join(output, "SHA256SUMS"))
	if err != nil {
		return err
	}
	defer file.Close()
	for _, name := range names {
		hash, err := release.SHA256File(filepath.Join(output, name))
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(file, "%s  %s\n", hash, name); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys(files map[string]string) []string {
	names := make([]string, 0, len(files)+1)
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func fatal(message string) { fmt.Fprintln(os.Stderr, message); os.Exit(1) }
