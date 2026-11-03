package relverify

// layoutFile is one file a bundle may hold, below its top-level directory.
type layoutFile struct {
	name string
	mode uint32
	// elf marks the two programs, which must be ELF64 little-endian
	// executables for machine 0xB7.
	elf bool
}

// layout returns the exact set of files of a bundle shape: the legacy shape
// (four files, plus the directory) or, with components, the same plus the
// update helper and its three units. The list is the allow-list of ADR 0010 U5.
func layout(components bool) []layoutFile {
	l := []layoutFile{
		{name: "LICENSE", mode: 0o644},
		{name: "THIRD_PARTY_NOTICES.txt", mode: 0o644},
		{name: "oarfish", mode: 0o755, elf: true},
		{name: "oarfish.service", mode: 0o644},
	}
	if components {
		l = append(l,
			layoutFile{name: "oarfish-update-helper", mode: 0o755, elf: true},
			layoutFile{name: "oarfish-update-helper.service", mode: 0o644},
			layoutFile{name: "oarfish-update-helper.socket", mode: 0o644},
			layoutFile{name: "oarfish-update-recover.service", mode: 0o644},
		)
	}
	return l
}

func lookupLayout(name string, components bool) (layoutFile, bool) {
	for _, f := range layout(components) {
		if f.name == name {
			return f, true
		}
	}
	return layoutFile{}, false
}

// isELFArm64 applies the installer's check_binary (install/install.sh) to the
// first bytes of a file: the magic "\x7fELF", class 64-bit (2), data
// little-endian (1), and machine 0xB7 (AArch64) in e_machine at offset 18.
func isELFArm64(head []byte) bool {
	return len(head) >= 20 && string(head[:6]) == "\x7fELF\x02\x01" && head[18] == 0xb7 && head[19] == 0x00
}
