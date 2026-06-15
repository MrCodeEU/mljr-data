package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"mljr-data/generator/internal/types"
)

// imageExt matches files this step cares about: existing webp (left in
// place, just reordered) and raster formats that get optimized to webp.
var imageExt = map[string]bool{
	".webp": true,
	".png":  true,
	".jpg":  true,
	".jpeg": true,
}

var leadingDigits = regexp.MustCompile(`^\d+`)

// syncProjectAssets scans assets/portfolio/<id>/ for each curated project.
// If the folder exists, every image in it is optimized to webp (non-webp
// files are re-encoded in place via cwebp and the original removed), the
// files are renumbered 0,1,2,... in natural order, and images is set to the
// resulting "/assets/portfolio/<id>/N.webp" paths. Projects without a local
// assets folder (e.g. picsum placeholders) are left untouched.
//
// Returns true if any project's images field changed, so the caller knows
// to rewrite projects.json.
func syncProjectAssets(root string, pf *types.ProjectsFile) (bool, error) {
	portfolioDir := filepath.Join(root, "assets", "portfolio")
	haveCwebp := true
	if _, err := exec.LookPath("cwebp"); err != nil {
		haveCwebp = false
		log.Println("assets: cwebp not found on PATH, skipping image optimization (renumbering existing webp only)")
	}

	changed := false
	for i := range pf.Curated {
		c := &pf.Curated[i]
		dir := filepath.Join(portfolioDir, c.ID)
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}

		entries, err := os.ReadDir(dir)
		if err != nil {
			return changed, fmt.Errorf("read %s: %w", dir, err)
		}

		var files []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if imageExt[ext] {
				files = append(files, e.Name())
			}
		}
		if len(files) == 0 {
			continue
		}
		sortNatural(files)

		if haveCwebp {
			for idx, name := range files {
				ext := strings.ToLower(filepath.Ext(name))
				if ext == ".webp" {
					continue
				}
				src := filepath.Join(dir, name)
				dst := filepath.Join(dir, strings.TrimSuffix(name, filepath.Ext(name))+".webp")
				if err := optimizeToWebp(src, dst); err != nil {
					return changed, fmt.Errorf("optimize %s: %w", src, err)
				}
				if err := os.Remove(src); err != nil {
					return changed, fmt.Errorf("remove %s: %w", src, err)
				}
				files[idx] = filepath.Base(dst)
			}
		}

		// Renumber to 0,1,2,... in natural order. Two passes via a temp
		// suffix avoid collisions when files are already partially numbered.
		sortNatural(files)
		tmpNames := make([]string, len(files))
		for idx, name := range files {
			tmp := name + ".tmp"
			if err := os.Rename(filepath.Join(dir, name), filepath.Join(dir, tmp)); err != nil {
				return changed, fmt.Errorf("rename %s: %w", name, err)
			}
			tmpNames[idx] = tmp
		}
		images := make([]string, len(tmpNames))
		for idx, tmp := range tmpNames {
			ext := filepath.Ext(strings.TrimSuffix(tmp, ".tmp"))
			if ext == "" {
				ext = ".webp"
			}
			final := fmt.Sprintf("%d%s", idx, ext)
			if err := os.Rename(filepath.Join(dir, tmp), filepath.Join(dir, final)); err != nil {
				return changed, fmt.Errorf("rename %s -> %s: %w", tmp, final, err)
			}
			images[idx] = "/assets/portfolio/" + c.ID + "/" + final
		}

		if !equalStrings(c.Images, images) {
			c.Images = images
			changed = true
		}
	}
	return changed, nil
}

// syncProfileAvatar looks for a single image file directly in assets/
// (sibling to portfolio/ and thesis/, not inside them). If found and not
// already webp, it's re-encoded to webp in place (original removed). The
// resulting "/assets/<name>.webp" path is returned so the caller can set it
// as the LinkedIn profile photo URL. Returns "" if no root-level image exists.
func syncProfileAvatar(root string) (string, error) {
	assetsDir := filepath.Join(root, "assets")
	entries, err := os.ReadDir(assetsDir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", assetsDir, err)
	}

	var candidates []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if imageExt[strings.ToLower(filepath.Ext(e.Name()))] {
			candidates = append(candidates, e.Name())
		}
	}
	if len(candidates) == 0 {
		return "", nil
	}
	sort.Strings(candidates)
	if len(candidates) > 1 {
		log.Printf("assets: multiple root-level images found (%s), using %q", strings.Join(candidates, ", "), candidates[0])
	}

	name := candidates[0]
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".webp" {
		src := filepath.Join(assetsDir, name)
		base := strings.TrimSuffix(name, filepath.Ext(name))
		dst := filepath.Join(assetsDir, base+".webp")
		if err := optimizeToWebp(src, dst); err != nil {
			return "", fmt.Errorf("optimize avatar %s: %w", src, err)
		}
		if err := os.Remove(src); err != nil {
			return "", fmt.Errorf("remove %s: %w", src, err)
		}
		name = base + ".webp"
	}
	return "/assets/" + name, nil
}

// optimizeToWebp re-encodes src (png/jpg) to dst as webp, downscaling to a
// max width of 1600px (no upscaling) at quality 82.
func optimizeToWebp(src, dst string) error {
	cmd := exec.Command("cwebp", "-quiet", "-resize", "1600", "0", "-q", "82", src, "-o", dst)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sortNatural sorts filenames so that numeric prefixes order numerically
// (0,1,2,...,10) rather than lexically (0,1,10,2,...).
func sortNatural(names []string) {
	sort.Slice(names, func(i, j int) bool {
		ai, bi := leadingDigits.FindString(names[i]), leadingDigits.FindString(names[j])
		if ai != "" && bi != "" {
			an, _ := strconv.Atoi(ai)
			bn, _ := strconv.Atoi(bi)
			if an != bn {
				return an < bn
			}
		}
		return names[i] < names[j]
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
