package toolchaindeps

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type packageJSONState struct {
	path   string
	data   []byte
	root   map[string]any
	err    error
	exists bool
	loaded bool
}

// NodeWorkspacePreflightIssue aggregates Node/Next.js workspace checks before
// install/build validation runs. Returns empty string when no issues found.
func NodeWorkspacePreflightIssue(workdir string) string {
	pkg := loadPackageJSON(workdir)
	var issues []string
	if issue := packageJSONPreflightIssueFromState(pkg); issue != "" {
		issues = append(issues, issue)
	}
	if issue := checkFakeBuildScriptWithState(pkg); issue != "" {
		issues = append(issues, issue)
	}
	if issue := checkPostCSSDepsWithState(workdir, pkg); issue != "" {
		issues = append(issues, issue)
	}
	if issue := checkConflictingNextPages(workdir); issue != "" {
		issues = append(issues, issue)
	}
	if issue := checkAppPagesRouterConflict(workdir); issue != "" {
		issues = append(issues, issue)
	}
	if len(issues) == 0 {
		return ""
	}
	return strings.Join(issues, "\n")
}

// CheckFakeBuildScript reports when package.json scripts.build is a no-op stub
// (e.g. "echo build successful") that would pass validation without compiling.
func CheckFakeBuildScript(workdir string) (ok bool, issue string) {
	return checkFakeBuildScriptForState(loadPackageJSON(workdir))
}

func checkFakeBuildScriptForState(pkg packageJSONState) (ok bool, issue string) {
	scripts, err := packageJSONScriptsFromState(pkg)
	if err != nil || scripts == nil {
		return true, ""
	}
	build, _ := scripts["build"].(string)
	build = strings.TrimSpace(build)
	if build == "" {
		return true, ""
	}
	if isNoOpBuildScript(build) {
		return false, fmt.Sprintf("[build script] package.json scripts.build is %q — replace with a real build command (e.g. \"next build\" for Next.js); echo/no-op scripts hide compile failures", build)
	}
	return true, ""
}

func checkFakeBuildScript(workdir string) string {
	ok, issue := checkFakeBuildScriptForState(loadPackageJSON(workdir))
	if ok {
		return ""
	}
	return issue
}

func checkFakeBuildScriptWithState(pkg packageJSONState) string {
	ok, issue := checkFakeBuildScriptForState(pkg)
	if ok {
		return ""
	}
	return issue
}

func isNoOpBuildScript(script string) bool {
	lower := strings.ToLower(strings.TrimSpace(script))
	if lower == "" {
		return false
	}
	if hasRealBuildCommand(lower) {
		return false
	}
	// Exact no-ops
	for _, noop := range []string{":", "true", "exit 0", "noop", "no-op"} {
		if lower == noop {
			return true
		}
	}
	// echo-only scripts (with or without quotes)
	if strings.HasPrefix(lower, "echo ") {
		return true
	}
	// Common Pod stub patterns
	if strings.Contains(lower, "build successful") {
		return true
	}
	return false
}

func hasRealBuildCommand(script string) bool {
	// If the script invokes a real build tool anywhere in the chain
	// (e.g. "next build && echo done"), do not treat it as a no-op.
	for _, marker := range []string{
		"next build",
		"vite build",
		"nuxt build",
		"svelte-kit build",
		"astro build",
		"webpack",
		"rollup",
		"tsc",
		"swc",
		"esbuild",
	} {
		if strings.Contains(script, marker) {
			return true
		}
	}
	return false
}

// CheckPostCSSDeps reports when postcss/tailwind config references packages
// missing from package.json dependencies or devDependencies.
func CheckPostCSSDeps(workdir string) (ok bool, issue string) {
	return checkPostCSSDepsForState(workdir, loadPackageJSON(workdir))
}

func checkPostCSSDepsForState(workdir string, pkg packageJSONState) (ok bool, issue string) {
	cfgPath := findPostCSSConfig(workdir)
	if cfgPath == "" {
		return true, ""
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return true, ""
	}
	content := string(data)
	required := postcssPluginsFromConfig(content)
	if len(required) == 0 {
		return true, ""
	}
	deps, err := packageJSONDepsFromState(pkg)
	if err != nil {
		return true, ""
	}
	var missing []string
	for _, pkg := range required {
		if !deps[pkg] {
			missing = append(missing, pkg)
		}
	}
	if len(missing) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("[postcss deps] %s references %v but package.json is missing: %s — add them to devDependencies before running dev/build",
		filepath.Base(cfgPath), required, strings.Join(missing, ", "))
}

func checkPostCSSDeps(workdir string) string {
	ok, issue := checkPostCSSDepsForState(workdir, loadPackageJSON(workdir))
	if ok {
		return ""
	}
	return issue
}

func checkPostCSSDepsWithState(workdir string, pkg packageJSONState) string {
	ok, issue := checkPostCSSDepsForState(workdir, pkg)
	if ok {
		return ""
	}
	return issue
}

func findPostCSSConfig(workdir string) string {
	for _, name := range []string{"postcss.config.js", "postcss.config.mjs", "postcss.config.cjs", "postcss.config.ts"} {
		p := filepath.Join(workdir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func postcssPluginsFromConfig(content string) []string {
	var plugins []string
	lower := strings.ToLower(content)
	for _, pkg := range []string{"tailwindcss", "autoprefixer", "@tailwindcss/postcss"} {
		if strings.Contains(lower, pkg) {
			plugins = append(plugins, pkg)
		}
	}
	return plugins
}

// CheckConflictingNextPages reports when the same App Router segment has
// multiple page files (e.g. app/page.js and app/page.tsx).
func CheckConflictingNextPages(workdir string) (ok bool, issue string) {
	appDir := filepath.Join(workdir, "app")
	if _, err := os.Stat(appDir); err != nil {
		return true, ""
	}
	var conflicts []string
	_ = filepath.WalkDir(appDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		entries, rerr := os.ReadDir(path)
		if rerr != nil {
			return nil
		}
		pages := make(map[string][]string)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "page.") {
				continue
			}
			ext := filepath.Ext(name)
			pages[ext] = append(pages[ext], name)
		}
		if len(pages) <= 1 {
			return nil
		}
		rel, _ := filepath.Rel(workdir, path)
		var names []string
		for _, ns := range pages {
			names = append(names, ns...)
		}
		conflicts = append(conflicts, fmt.Sprintf("%s has %s", rel, strings.Join(names, " + ")))
		return nil
	})
	if len(conflicts) == 0 {
		return true, ""
	}
	return false, fmt.Sprintf("[route conflict] multiple page files at the same App Router segment — keep exactly one page.* per route: %s",
		strings.Join(conflicts, "; "))
}

func checkConflictingNextPages(workdir string) string {
	ok, issue := CheckConflictingNextPages(workdir)
	if ok {
		return ""
	}
	return issue
}

// CheckAppPagesRouterConflict reports when both App Router and Pages Router
// define a root index route, which causes ambiguous routing.
func CheckAppPagesRouterConflict(workdir string) (ok bool, issue string) {
	hasAppRoot := hasRootAppPage(workdir)
	hasPagesRoot := fileExists(filepath.Join(workdir, "pages", "index.js")) ||
		fileExists(filepath.Join(workdir, "pages", "index.tsx")) ||
		fileExists(filepath.Join(workdir, "pages", "index.jsx")) ||
		fileExists(filepath.Join(workdir, "pages", "index.ts"))
	if hasAppRoot && hasPagesRoot {
		return false, "[router conflict] both app/ and pages/index.* define a root route — remove pages/ or migrate fully to App Router; mixed routers produce wrong pages and QA false positives"
	}
	return true, ""
}

func checkAppPagesRouterConflict(workdir string) string {
	ok, issue := CheckAppPagesRouterConflict(workdir)
	if ok {
		return ""
	}
	return issue
}

func hasRootAppPage(workdir string) bool {
	for _, name := range []string{"page.js", "page.jsx", "page.ts", "page.tsx", "page.mdx"} {
		if fileExists(filepath.Join(workdir, "app", name)) {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func readPackageJSONScripts(workdir string) (map[string]any, error) {
	return packageJSONScriptsFromState(loadPackageJSON(workdir))
}

func packageJSONDeps(workdir string) (map[string]bool, error) {
	return packageJSONDepsFromState(loadPackageJSON(workdir))
}

func loadPackageJSON(workdir string) packageJSONState {
	path := filepath.Join(workdir, PackageJSONRelPath)
	state := packageJSONState{path: path, loaded: true}
	data, err := os.ReadFile(path)
	if err != nil {
		state.err = err
		if os.IsNotExist(err) {
			return state
		}
		return state
	}
	state.exists = true
	state.data = data
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		state.err = err
		return state
	}
	state.root = root
	return state
}

func packageJSONPreflightIssueFromState(pkg packageJSONState) string {
	if !pkg.loaded {
		return ""
	}
	if pkg.err != nil {
		if os.IsNotExist(pkg.err) {
			return ""
		}
		if pkg.exists && len(pkg.data) > 0 {
			if ok, issue := ValidatePackageJSONBytes(pkg.data, pkg.path); !ok {
				return fmt.Sprintf("[package.json] %s — fix the manifest before re-running install_dependencies or assigning another install task", issue)
			}
		}
		return fmt.Sprintf("[package.json] package.json: read %s: %v — fix the manifest before re-running install_dependencies or assigning another install task", pkg.path, pkg.err)
	}
	if !pkg.exists {
		return ""
	}
	if ok, issue := ValidatePackageJSONBytes(pkg.data, pkg.path); !ok {
		return fmt.Sprintf("[package.json] %s — fix the manifest before re-running install_dependencies or assigning another install task", issue)
	}
	return ""
}

func packageJSONScriptsFromState(pkg packageJSONState) (map[string]any, error) {
	if pkg.err != nil {
		return nil, pkg.err
	}
	if !pkg.exists {
		return nil, os.ErrNotExist
	}
	scripts, _ := pkg.root["scripts"].(map[string]any)
	return scripts, nil
}

func packageJSONDepsFromState(pkg packageJSONState) (map[string]bool, error) {
	if pkg.err != nil {
		return nil, pkg.err
	}
	if !pkg.exists {
		return nil, os.ErrNotExist
	}
	deps := make(map[string]bool)
	for _, key := range []string{"dependencies", "devDependencies", "peerDependencies", "optionalDependencies"} {
		section, _ := pkg.root[key].(map[string]any)
		for name := range section {
			deps[name] = true
		}
	}
	return deps, nil
}
