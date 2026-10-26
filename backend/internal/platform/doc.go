// Package platform is the root of the detection packages (Pflichtenheft 8.3):
//
//	source            read-only, bounded file access; the only place that touches the host
//	hardware          board, SoC family, architecture, memory (device tree, cpuinfo, DMI)
//	operating_system  /etc/os-release, kernel, OS adapters (Raspberry Pi OS, Ubuntu, Debian family)
//	capabilities      what the running system can do, found by probing standard interfaces
//	profiles          embedded, schema-validated SoC and board profiles
//	detector          combines the parts above and computes the support tier
//
// Hardware and operating-system detection never import each other. Detection
// only reads files; it never runs a program (docs/DETECTION.md, ADR 0007).
package platform
