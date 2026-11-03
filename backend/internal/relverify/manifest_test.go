package relverify

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func manifestBytes(t testing.TB, edit func(m map[string]any)) []byte {
	t.Helper()
	r := newRelease(t, "0.1.0-test.1", true)
	if edit != nil {
		edit(r.man)
	}
	b, err := json.MarshalIndent(r.man, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func TestManifestValid(t *testing.T) {
	for _, comps := range []bool{false, true} {
		r := newRelease(t, "0.1.0-test.1", comps)
		b, _ := json.Marshal(r.man)
		m, err := ParseManifest(b)
		if err != nil {
			t.Fatalf("components=%v: %v (%s)", comps, err, detail(err))
		}
		if m.Version != "0.1.0-test.1" || m.Tag != "v0.1.0-test.1" || !m.Prerelease || m.Commit != testCommit {
			t.Fatalf("%+v", m)
		}
		if (m.Components != nil) != comps {
			t.Fatal("components")
		}
		if len(m.Contents) != map[bool]int{false: 4, true: 8}[comps] {
			t.Fatalf("%d contents", len(m.Contents))
		}
		if m.Requirements.Libc == "" || m.Requirements.CPU == "" || m.Requirements.RuntimeDependencies == nil {
			t.Fatal("libc, cpu and runtime dependencies are exposed")
		}
		if v, err := m.Semver(); err != nil || v.String() != m.Version {
			t.Fatal("Semver")
		}
	}
}

func TestManifestStableVersion(t *testing.T) {
	r := newRelease(t, "1.2.3", false)
	b, _ := json.Marshal(r.man)
	m, err := ParseManifest(b)
	if err != nil || m.Prerelease {
		t.Fatalf("%v", err)
	}
}

// One failing value for every field of the table in the task, and for the
// structure around them.
func TestManifestFields(t *testing.T) {
	type edit struct {
		name string
		do   func(t *testing.T, m map[string]any)
		want Code
	}
	set := func(path string, v any) func(*testing.T, map[string]any) {
		return func(t *testing.T, m map[string]any) { setPath(t, m, path, v) }
	}
	del := func(path string) func(*testing.T, map[string]any) {
		return func(t *testing.T, m map[string]any) { delPath(t, m, path) }
	}
	hash := strings.Repeat("a", 64)
	cases := []edit{
		{"schema_version 2", set("schema_version", 2), CodeManifestInvalid},
		{"schema_version 0", set("schema_version", 0), CodeManifestInvalid},
		{"schema_version as string", set("schema_version", "1"), CodeManifestInvalid},
		{"schema_version 1.5", set("schema_version", 1.5), CodeManifestInvalid},
		{"name other", set("name", "oarfish2"), CodeManifestInvalid},
		{"name upper case", set("name", "Oarfish"), CodeManifestInvalid},
		{"version not semver", set("version", "1.0"), CodeManifestInvalid},
		{"version with build metadata", set("version", "0.1.0-test.1+b"), CodeManifestInvalid},
		{"version leading v", set("version", "v0.1.0-test.1"), CodeManifestInvalid},
		{"tag without v", set("tag", "0.1.0-test.1"), CodeManifestInvalid},
		{"tag other version", set("tag", "v0.1.0-test.2"), CodeManifestInvalid},
		{"prerelease false for a prerelease version", set("prerelease", false), CodeManifestInvalid},
		{"prerelease as string", set("prerelease", "true"), CodeManifestInvalid},
		{"commit 39 digits", set("commit", testCommit[:39]), CodeManifestInvalid},
		{"commit 41 digits", set("commit", testCommit+"0"), CodeManifestInvalid},
		{"commit upper case", set("commit", strings.ToUpper(testCommit)), CodeManifestInvalid},
		{"commit not hex", set("commit", strings.Repeat("g", 40)), CodeManifestInvalid},
		{"build_date empty", set("build_date", ""), CodeManifestInvalid},
		{"build_date control character", set("build_date", "2023\n"), CodeManifestInvalid},
		{"os darwin", set("os", "darwin"), CodeRequirementsNotMet},
		{"os empty", set("os", ""), CodeRequirementsNotMet},
		{"architecture amd64", set("architecture", "amd64"), CodeRequirementsNotMet},
		{"architecture aarch64", set("architecture", "aarch64"), CodeRequirementsNotMet},
		{"requirements.os darwin", set("requirements.os", "darwin"), CodeRequirementsNotMet},
		{"requirements.architecture amd64", set("requirements.architecture", "amd64"), CodeRequirementsNotMet},
		{"requirements.cpu empty", set("requirements.cpu", ""), CodeManifestInvalid},
		{"requirements.libc empty", set("requirements.libc", ""), CodeManifestInvalid},
		{"requirements.libc too long", set("requirements.libc", strings.Repeat("a", 257)), CodeManifestInvalid},
		{"runtime_dependencies null", set("requirements.runtime_dependencies", nil), CodeManifestInvalid},
		{"runtime_dependencies not strings", set("requirements.runtime_dependencies", []any{1}), CodeManifestInvalid},
		{"runtime_dependencies empty string", set("requirements.runtime_dependencies", []any{""}), CodeManifestInvalid},
		{"runtime_dependencies 17", set("requirements.runtime_dependencies", func() []any {
			var l []any
			for i := 0; i < 17; i++ {
				l = append(l, "x")
			}
			return l
		}()), CodeManifestInvalid},
		{"min_installed_version invalid", set("requirements.min_installed_version", "1.0"), CodeManifestInvalid},
		{"min_installed_version empty", set("requirements.min_installed_version", ""), CodeManifestInvalid},
		{"min_installed_version build metadata", set("requirements.min_installed_version", "1.0.0+x"), CodeManifestInvalid},
		{"min_installed_version number", set("requirements.min_installed_version", 1), CodeManifestInvalid},
		{"helper_protocol 0", set("requirements.helper_protocol", 0), CodeManifestInvalid},
		{"helper_protocol negative", set("requirements.helper_protocol", -1), CodeManifestInvalid},
		{"helper_protocol string", set("requirements.helper_protocol", "1"), CodeManifestInvalid},
		{"helper_protocol fraction", set("requirements.helper_protocol", 1.5), CodeManifestInvalid},
		{"components.key_id 15 digits", set("components.key_id", "11f1c52777090bf"), CodeManifestInvalid},
		{"components.key_id upper case", set("components.key_id", "11F1C52777090BFC"), CodeManifestInvalid},
		{"components.key_id not hex", set("components.key_id", "11f1c52777090bfz"), CodeManifestInvalid},
		{"components.key_id missing", del("components.key_id"), CodeManifestInvalid},
		{"components.protocol 0", set("components.protocol", 0), CodeManifestInvalid},
		{"components.protocol missing", del("components.protocol"), CodeManifestInvalid},
		{"components.protocol string", set("components.protocol", "1"), CodeManifestInvalid},
		{"components not an object", set("components", 1), CodeManifestInvalid},
		{"bundle.file other version", set("bundle.file", "oarfish_0.1.0-test.2_linux_arm64.tar.gz"), CodeManifestInvalid},
		{"bundle.file other arch", set("bundle.file", "oarfish_0.1.0-test.1_linux_amd64.tar.gz"), CodeManifestInvalid},
		{"bundle.file with a path", set("bundle.file", "x/oarfish_0.1.0-test.1_linux_arm64.tar.gz"), CodeManifestInvalid},
		{"bundle.sha256 63 digits", set("bundle.sha256", hash[:63]), CodeManifestInvalid},
		{"bundle.sha256 upper case", set("bundle.sha256", strings.ToUpper(hash)), CodeManifestInvalid},
		{"bundle.size 0", set("bundle.size", 0), CodeManifestInvalid},
		{"bundle.size negative", set("bundle.size", -5), CodeManifestInvalid},
		{"bundle.size string", set("bundle.size", "100"), CodeManifestInvalid},
		{"contents path outside the directory", set("contents.0.path", "other_dir/LICENSE"), CodeManifestInvalid},
		{"contents path with ..", set("contents.0.path", "oarfish_0.1.0-test.1_linux_arm64/../LICENSE"), CodeManifestInvalid},
		{"contents path absolute", set("contents.0.path", "/oarfish_0.1.0-test.1_linux_arm64/LICENSE"), CodeManifestInvalid},
		{"contents path not allow-listed", set("contents.0.path", "oarfish_0.1.0-test.1_linux_arm64/evil"), CodeManifestInvalid},
		{"contents path duplicate", set("contents.1.path", "oarfish_0.1.0-test.1_linux_arm64/LICENSE"), CodeManifestInvalid},
		{"contents mode 0777", set("contents.0.mode", "0777"), CodeManifestInvalid},
		{"contents mode 644", set("contents.0.mode", "644"), CodeManifestInvalid},
		{"contents mode 0600", set("contents.0.mode", "0600"), CodeManifestInvalid},
		{"contents mode as number", set("contents.0.mode", 420), CodeManifestInvalid},
		{"contents mode 0755 for a text file", set("contents.0.mode", "0755"), CodeManifestInvalid},
		{"contents mode 0644 for a program", func() func(*testing.T, map[string]any) {
			return func(t *testing.T, m map[string]any) {
				for i, c := range m["contents"].([]any) {
					if strings.HasSuffix(c.(map[string]any)["path"].(string), "/oarfish") {
						setPath(t, m, "contents."+string(rune('0'+i))+".mode", "0644")
					}
				}
			}
		}(), CodeManifestInvalid},
		{"contents size 0", set("contents.0.size", 0), CodeManifestInvalid},
		{"contents size negative", set("contents.0.size", -1), CodeManifestInvalid},
		{"contents size over 128 MiB", set("contents.0.size", maxContentBytes+1), CodeManifestInvalid},
		{"contents sizes add up to over 128 MiB", func(t *testing.T, m map[string]any) {
			setPath(t, m, "contents.0.size", maxContentBytes-10)
			setPath(t, m, "contents.1.size", 11)
		}, CodeManifestInvalid},
		{"contents sha256 short", set("contents.0.sha256", hash[:60]), CodeManifestInvalid},
		{"contents sha256 upper case", set("contents.0.sha256", strings.ToUpper(hash)), CodeManifestInvalid},
		{"contents entry without path", del("contents.0.path"), CodeManifestInvalid},
		{"contents entry without mode", del("contents.0.mode"), CodeManifestInvalid},
		{"contents entry without size", del("contents.0.size"), CodeManifestInvalid},
		{"contents entry without sha256", del("contents.0.sha256"), CodeManifestInvalid},
		{"contents entry not an object", set("contents.0", "x"), CodeManifestInvalid},
		{"contents empty", set("contents", []any{}), CodeManifestInvalid},
		{"contents null", set("contents", nil), CodeManifestInvalid},
		{"contents one entry short", func(t *testing.T, m map[string]any) {
			m["contents"] = m["contents"].([]any)[1:]
		}, CodeManifestInvalid},
		{"contents one entry too many", func(t *testing.T, m map[string]any) {
			c := m["contents"].([]any)
			m["contents"] = append(c, c[0])
		}, CodeManifestInvalid},
	}
	// Every required field, deleted.
	for _, f := range []string{"schema_version", "name", "version", "tag", "prerelease", "commit", "build_date", "os",
		"architecture", "requirements", "bundle", "contents", "requirements.os", "requirements.architecture",
		"requirements.cpu", "requirements.libc", "requirements.runtime_dependencies", "bundle.file", "bundle.sha256", "bundle.size"} {
		cases = append(cases, edit{"missing " + f, del(f), CodeManifestInvalid})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRelease(t, "0.1.0-test.1", true)
			tc.do(t, r.man)
			b, _ := json.Marshal(r.man)
			_, err := ParseManifest(b)
			wantCode(t, err, tc.want)
		})
	}
}

// The components object is present if and only if the contents have the
// component files.
func TestManifestShape(t *testing.T) {
	t.Run("legacy contents with a components object", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", false)
		r.man["components"] = map[string]any{"key_id": "11f1c52777090bfc", "protocol": 1}
		b, _ := json.Marshal(r.man)
		_, err := ParseManifest(b)
		wantDetail(t, err, CodeManifestInvalid, "shape")
	})
	t.Run("components contents without a components object", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", true)
		delete(r.man, "components")
		b, _ := json.Marshal(r.man)
		_, err := ParseManifest(b)
		wantDetail(t, err, CodeManifestInvalid, "shape")
	})
	t.Run("components object, one component file missing", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", true)
		c := r.man["contents"].([]any)
		var keep []any
		for _, e := range c {
			if !strings.HasSuffix(e.(map[string]any)["path"].(string), "oarfish-update-recover.service") {
				keep = append(keep, e)
			}
		}
		r.man["contents"] = keep
		b, _ := json.Marshal(r.man)
		_, err := ParseManifest(b)
		wantCode(t, err, CodeManifestInvalid)
	})
	t.Run("legacy contents with one component file", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", false)
		r.man["contents"] = append(r.man["contents"].([]any), map[string]any{
			"path": r.root + "/oarfish-update-helper", "mode": "0755", "size": 100, "sha256": strings.Repeat("a", 64)})
		b, _ := json.Marshal(r.man)
		_, err := ParseManifest(b)
		wantCode(t, err, CodeManifestInvalid)
	})
	t.Run("components object null is no components object", func(t *testing.T) {
		r := newRelease(t, "0.1.0-test.1", false)
		r.man["components"] = nil
		b, _ := json.Marshal(r.man)
		m, err := ParseManifest(b)
		if err != nil || m.Components != nil {
			t.Fatal(err)
		}
	})
}

func TestManifestOptionalFields(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	setPath(t, r.man, "requirements.min_installed_version", "0.1.0-alpha.6")
	setPath(t, r.man, "requirements.helper_protocol", 2)
	b, _ := json.Marshal(r.man)
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Requirements.MinInstalledVersion != "0.1.0-alpha.6" || m.Requirements.HelperProtocol != 2 {
		t.Fatalf("%+v", m.Requirements)
	}
	if m.Components == nil || m.Components.KeyID != "11f1c52777090bfc" || m.Components.Protocol != 1 {
		t.Fatalf("%+v", m.Components)
	}
	// Absent: the zero values.
	r = newRelease(t, "0.1.0-test.1", false)
	b, _ = json.Marshal(r.man)
	m, _ = ParseManifest(b)
	if m.Requirements.MinInstalledVersion != "" || m.Requirements.HelperProtocol != 0 {
		t.Fatal("optional fields are not absent")
	}
}

// Unknown fields are ignored on the device (the consumer), where bundlegen
// check, the producer, refuses them.
func TestManifestUnknownFieldsIgnored(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	r.man["future_field"] = map[string]any{"a": []any{1, 2, map[string]any{"b": nil}}}
	r.man["requirements"].(map[string]any)["kernel_min"] = "6.1"
	r.man["bundle"].(map[string]any)["signature"] = "x"
	r.man["components"].(map[string]any)["extra"] = true
	r.man["contents"].([]any)[0].(map[string]any)["owner"] = "root"
	b, _ := json.Marshal(r.man)
	if _, err := ParseManifest(b); err != nil {
		t.Fatalf("%v (%s)", err, detail(err))
	}
	// A new value of an unknown field never matters; a new value of a known
	// field does.
	setPath(t, r.man, "schema_version", 2)
	b, _ = json.Marshal(r.man)
	_, err := ParseManifest(b)
	wantCode(t, err, CodeManifestInvalid)
}

func TestManifestJSONShape(t *testing.T) {
	good := manifestBytes(t, nil)
	for name, b := range map[string][]byte{
		"empty":               nil,
		"null":                []byte("null"),
		"array":               []byte("[]"),
		"string":              []byte(`"x"`),
		"empty object":        []byte("{}"),
		"truncated":           good[:len(good)/2],
		"trailing garbage":    append(append([]byte(nil), good...), "x"...),
		"two documents":       append(append([]byte(nil), good...), good...),
		"trailing comma":      bytes.Replace(good, []byte("\"0.1.0-test.1\"\n}"), []byte("\"0.1.0-test.1\",\n}"), 1),
		"larger than 64 KiB":  append(append([]byte(nil), good...), bytes.Repeat([]byte(" "), maxManifestFile)...),
		"not UTF-8 in a name": bytes.Replace(good, []byte(`"oarfish"`), []byte("\"oarfish\xff\""), 1),
		"BOM":                 append([]byte("\xef\xbb\xbf"), good...),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParseManifest(b)
			wantCode(t, err, CodeManifestInvalid)
		})
	}
	t.Run("leading and trailing white space is fine", func(t *testing.T) {
		if _, err := ParseManifest(append(append([]byte("\n\t "), good...), "\n\n"...)); err != nil {
			t.Fatal(err)
		}
	})
}

// encoding/json takes the last of two equal keys and matches names without
// regard to case; the manifest reader refuses both.
func TestManifestDuplicateAndCaseKeys(t *testing.T) {
	good := string(manifestBytes(t, nil))
	cases := map[string]string{
		"duplicate top-level key":       strings.Replace(good, `"name": "oarfish",`, `"name": "oarfish", "name": "oarfish",`, 1),
		"duplicate key, other value":    strings.Replace(good, `"name": "oarfish",`, `"name": "evil", "name": "oarfish",`, 1),
		"duplicate nested key":          strings.Replace(good, `"os": "linux",`, `"os": "linux", "os": "linux",`, 1),
		"duplicate in a contents entry": strings.Replace(good, `"mode": "0644",`, `"mode": "0644", "mode": "0644",`, 1),
		"keys differing in case":        strings.Replace(good, `"name": "oarfish",`, `"name": "oarfish", "NAME": "evil",`, 1),
		"upper-case known key":          strings.Replace(good, `"schema_version": 1,`, `"Schema_Version": 1,`, 1),
		"capitalised version":           strings.Replace(good, `"version": "0.1.0-test.1"`, `"Version": "0.1.0-test.1"`, 1),
		"capitalised nested key":        strings.Replace(good, `"architecture": "arm64",`+"\n    \"cpu\"", `"Architecture": "arm64",`+"\n    \"cpu\"", 1),
		"escaped duplicate":             strings.Replace(good, `"name": "oarfish",`, `"name": "oarfish", "name": "oarfish",`, 1),
	}
	for name, doc := range cases {
		if doc == good {
			t.Fatalf("%s: the edit did not apply", name)
		}
		t.Run(name, func(t *testing.T) {
			_, err := ParseManifest([]byte(doc))
			wantCode(t, err, CodeManifestInvalid)
		})
	}
	t.Run("deep nesting of an unknown field", func(t *testing.T) {
		deep := strings.Repeat("[", 20) + strings.Repeat("]", 20)
		doc := strings.Replace(good, `"schema_version": 1,`, `"schema_version": 1, "x": `+deep+`,`, 1)
		_, err := ParseManifest([]byte(doc))
		wantCode(t, err, CodeManifestInvalid)
	})
	t.Run("same key in different objects is fine", func(t *testing.T) {
		if _, err := ParseManifest([]byte(good)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestManifestCheck(t *testing.T) {
	r := newRelease(t, "0.1.0-test.1", true)
	b, _ := json.Marshal(r.man)
	m, err := ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	ok := Expect{Version: "0.1.0-test.1", Architecture: "arm64", BundleFile: r.bundleName(), BundleSHA256: SumHex(r.bundle)}
	if err := m.Check(ok); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		mut  func(e *Expect)
		want Code
	}{
		{"another requested version", func(e *Expect) { e.Version = "0.1.0-test.2" }, CodeManifestInvalid},
		{"another architecture", func(e *Expect) { e.Architecture = "amd64" }, CodeRequirementsNotMet},
		{"another bundle name", func(e *Expect) { e.BundleFile = "oarfish_0.1.0-test.2_linux_arm64.tar.gz" }, CodeManifestInvalid},
		{"another listed hash", func(e *Expect) { e.BundleSHA256 = strings.Repeat("0", 64) }, CodeChecksumMismatch},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := ok
			tc.mut(&e)
			wantCode(t, m.Check(e), tc.want)
		})
	}
}
