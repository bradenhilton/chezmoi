//go:build !noupgrade && unix

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"runtime"
	"strings"

	"github.com/coreos/go-semver/semver"
	"github.com/google/go-github/v61/github"
	vfs "github.com/twpayne/go-vfs/v5"

	"github.com/twpayne/chezmoi/internal/chezmoi"
)

const (
	packageTypeNone = ""
	packageTypeAPK  = "apk"
	packageTypeAUR  = "aur"
	packageTypeDEB  = "deb"
	packageTypeRPM  = "rpm"
)

var (
	packageTypeByID = map[string]string{
		"alpine":   packageTypeAPK,
		"amzn":     packageTypeRPM,
		"arch":     packageTypeAUR,
		"centos":   packageTypeRPM,
		"fedora":   packageTypeRPM,
		"opensuse": packageTypeRPM,
		"debian":   packageTypeDEB,
		"rhel":     packageTypeRPM,
		"sles":     packageTypeRPM,
		"ubuntu":   packageTypeDEB,
	}

	archReplacements = map[string]map[string]string{
		packageTypeDEB: {
			"386": "i386",
			"arm": "armel",
		},
		packageTypeRPM: {
			"amd64": "x86_64",
			"386":   "i686",
			"arm":   "armhfp",
			"arm64": "aarch64",
		},
	}
)

func (c *Config) brewUpgrade() error {
	return c.run(chezmoi.EmptyAbsPath, "brew", []string{"upgrade", "chezmoi"})
}

func (c *Config) getPackageFilename(packageType string, version *semver.Version, goOS, arch string) (string, error) {
	if archReplacement, ok := archReplacements[packageType][arch]; ok {
		arch = archReplacement
	}
	switch packageType {
	case packageTypeAPK:
		return fmt.Sprintf("chezmoi_%s_%s_%s.apk", version, goOS, arch), nil
	case packageTypeDEB:
		return fmt.Sprintf("chezmoi_%s_%s_%s.deb", version, goOS, arch), nil
	case packageTypeRPM:
		return fmt.Sprintf("chezmoi-%s-%s.rpm", version, arch), nil
	default:
		return "", fmt.Errorf("%s: unsupported package type", packageType)
	}
}

func (c *Config) snapRefresh() error {
	return c.run(chezmoi.EmptyAbsPath, "snap", []string{"refresh", "chezmoi"})
}

func (c *Config) upgradeUNIXPackage(
	ctx context.Context,
	version *semver.Version,
	rr *github.RepositoryRelease,
	useSudo bool,
) error {
	switch runtime.GOOS {
	case "linux":
		// Determine the package type and architecture.
		packageType, err := getPackageType(c.baseSystem)
		if err != nil {
			return err
		}

		// chezmoi does not build and distribute AUR packages, so instead rely
		// on pacman and the community package.
		if packageType == packageTypeAUR {
			var args []string
			if useSudo {
				args = append(args, "sudo")
			}
			args = append(args, "pacman", "-S", "--needed", "chezmoi")
			return c.run(chezmoi.EmptyAbsPath, args[0], args[1:])
		}

		// Find the release asset.
		packageFilename, err := c.getPackageFilename(packageType, version, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return err
		}
		releaseAsset := getReleaseAssetByName(rr, packageFilename)
		if releaseAsset == nil {
			return fmt.Errorf("%s: cannot find release asset", packageFilename)
		}

		// Create a temporary directory for the package.
		tempDirAbsPath, err := c.tempDir("chezmoi")
		if err != nil {
			return err
		}

		data, err := c.downloadURL(ctx, releaseAsset.GetBrowserDownloadURL())
		if err != nil {
			return err
		}
		if err := c.verifyChecksum(ctx, rr, releaseAsset.GetName(), data); err != nil {
			return err
		}

		packageAbsPath := tempDirAbsPath.JoinString(releaseAsset.GetName())
		if err := c.baseSystem.WriteFile(packageAbsPath, data, 0o644); err != nil {
			return err
		}

		// Install the package from disk.
		var args []string
		if useSudo {
			args = append(args, "sudo")
		}
		switch packageType {
		case packageTypeAPK:
			args = append(args, "apk", "--allow-untrusted", packageAbsPath.String())
		case packageTypeDEB:
			args = append(args, "dpkg", "-i", packageAbsPath.String())
		case packageTypeRPM:
			args = append(args, "rpm", "-U", packageAbsPath.String())
		}
		return c.run(chezmoi.EmptyAbsPath, args[0], args[1:])
	default:
		return fmt.Errorf("%s: unsupported GOOS", runtime.GOOS)
	}
}

func (c *Config) winGetUpgrade() error {
	return errUnsupportedUpgradeMethod
}

// getPackageType returns the distributions package type based on is OS release.
func getPackageType(system chezmoi.System) (string, error) {
	osRelease, err := chezmoi.OSRelease(system.UnderlyingFS())
	if err != nil {
		return packageTypeNone, err
	}
	if id, ok := osRelease["ID"].(string); ok {
		if packageType, ok := packageTypeByID[id]; ok {
			return packageType, nil
		}
	}
	if idLikes, ok := osRelease["ID_LIKE"].(string); ok {
		for _, id := range strings.Split(idLikes, " ") {
			if packageType, ok := packageTypeByID[id]; ok {
				return packageType, nil
			}
		}
	}
	err = fmt.Errorf(
		"could not determine package type (ID=%q, ID_LIKE=%q)",
		osRelease["ID"],
		osRelease["ID_LIKE"],
	)
	return packageTypeNone, err
}

// getUpgradeMethod attempts to determine the method by which chezmoi can be
// upgraded by looking at how it was installed.
func getUpgradeMethod(fileSystem vfs.Stater, executableAbsPath chezmoi.AbsPath) (string, error) {
	switch {
	case runtime.GOOS == "darwin" && strings.Contains(executableAbsPath.String(), "/homebrew/"):
		return upgradeMethodBrewUpgrade, nil
	case runtime.GOOS == "linux" && strings.Contains(executableAbsPath.String(), "/.linuxbrew/"):
		return upgradeMethodBrewUpgrade, nil
	}

	// If the executable is in the user's home directory, then always use
	// replace-executable.
	switch userHomeDir, err := chezmoi.UserHomeDir(); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", err
	default:
		switch executableInUserHomeDir, err := vfs.Contains(fileSystem, executableAbsPath.String(), userHomeDir); {
		case errors.Is(err, fs.ErrNotExist):
		case err != nil:
			return "", err
		case executableInUserHomeDir:
			return upgradeMethodReplaceExecutable, nil
		}
	}

	// If the executable is in the system's temporary directory, then always use
	// replace-executable.
	if executableIsInTempDir, err := vfs.Contains(fileSystem, executableAbsPath.String(), os.TempDir()); err != nil {
		return "", err
	} else if executableIsInTempDir {
		return upgradeMethodReplaceExecutable, nil
	}

	switch runtime.GOOS {
	case "darwin":
		return upgradeMethodReplaceExecutable, nil
	case "freebsd":
		return upgradeMethodReplaceExecutable, nil
	case "linux":
		if ok, _ := vfs.Contains(fileSystem, executableAbsPath.String(), "/snap"); ok {
			return upgradeMethodSnapRefresh, nil
		}
		fileInfo, err := fileSystem.Stat(executableAbsPath.String())
		if err != nil {
			return "", err
		}
		uid := os.Getuid()
		switch fileInfoUID(fileInfo) {
		case 0:
			method := upgradeMethodUpgradePackage
			if uid != 0 {
				if _, err := chezmoi.LookPath("sudo"); err == nil {
					method = upgradeMethodSudoPrefix + method
				}
			}
			return method, nil
		case uid:
			return upgradeMethodReplaceExecutable, nil
		default:
			return "", fmt.Errorf("%s: cannot upgrade executable owned by non-current non-root user", executableAbsPath)
		}
	case "openbsd":
		return upgradeMethodReplaceExecutable, nil
	default:
		return "", nil
	}
}
