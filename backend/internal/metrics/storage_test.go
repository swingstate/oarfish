package metrics

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
)

// Task ST1 (ORB-STO-001 to -003): the device and disk of a real file system,
// the removable flag, and the other mounts of the page Speicher.

const (
	mmcLink = "../../devices/platform/soc/3f202000.mmc/mmc_host/mmc0/mmc0:0001/block/mmcblk0/"
	usbLink = "../../devices/platform/soc/3f980000.usb/usb1/1-1/1-1:1.0/host0/target0:0:0/0:0:0:0/block/sda/"
)

func link(target string) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(target), Mode: fs.ModeSymlink}
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

// refStorage is the reference layout: the card with root and boot partitions.
func refStorage(t *testing.T) fstest.MapFS {
	fsys := cloneFixture(t, "pi-zero-2w")
	fsys["sys/dev/block/179:2"] = link(mmcLink + "mmcblk0p2")
	fsys["sys/dev/block/179:1"] = link(mmcLink + "mmcblk0p1")
	fsys["sys/block/mmcblk0/removable"] = file("0\n")
	// Identity files of the card: never opened.
	fsys["sys/block/mmcblk0/device/serial"] = file("0x00000000\n")
	fsys["sys/block/mmcblk0/device/cid"] = file("00000000000000000000000000000000\n")
	fsys["sys/block/mmcblk0/device/csd"] = file("00000000000000000000000000000000\n")
	return fsys
}

// statfsAll answers every mount point with a fixed size and records the calls.
type statfsAll struct {
	mu    sync.Mutex
	calls []string
}

func (s *statfsAll) fn(path string) (Statfs, error) {
	s.mu.Lock()
	s.calls = append(s.calls, path)
	s.mu.Unlock()
	return Statfs{BlockSize: 4096, Blocks: 1000, Free: 400, Available: 400}, nil
}

func TestStorageReferenceLayout(t *testing.T) {
	fsys := refStorage(t)
	rec := &recorder{FS: fsys}
	st := &statfsAll{}
	s := NewCollector(rec, st.fn).Collect(t0)
	if len(s.Storage) != 2 {
		t.Fatalf("storage = %s", mustJSON(t, s.Storage))
	}
	root, boot := s.Storage[0], s.Storage[1]
	if root.MountPoint != "/" || root.Device != "mmcblk0p2" || root.Disk != "mmcblk0" || root.Removable == nil || *root.Removable || root.USB {
		t.Errorf("root = %s", mustJSON(t, root))
	}
	if boot.MountPoint != "/boot/firmware" || boot.Device != "mmcblk0p1" || boot.Disk != "mmcblk0" {
		t.Errorf("boot = %s", mustJSON(t, boot))
	}
	// /tmp is a tmpfs: listed as memory, with its size; /run is a system area.
	if got := mustJSON(t, s.Mounts); got != `[{"mount_point":"/tmp","fs_type":"tmpfs","kind":"memory","total_bytes":4096000,"used_bytes":2457600,"available_bytes":1638400,"used_percent":60}]` {
		t.Errorf("mounts = %s", got)
	}
	for _, n := range []string{"sys/block/mmcblk0/device/serial", "sys/block/mmcblk0/device/cid", "sys/block/mmcblk0/device/csd", "sys/dev/block/179:2/uevent"} {
		if rec.opened(n) {
			t.Errorf("opened %s", n)
		}
	}
	if !rec.opened("sys/block/mmcblk0/removable") {
		t.Error("the removable flag was not read")
	}
}

// A USB stick: device and disk from the link, removable 1, on a USB bus; the
// user name in the mount point is not shown.
func TestStorageUSBStick(t *testing.T) {
	fsys := refStorage(t)
	fsys["proc/self/mountinfo"] = file(string(fsys["proc/self/mountinfo"].Data) +
		"40 22 8:1 / /media/alice-secret/STICK rw,nosuid,nodev - vfat /dev/sda1 rw\n")
	fsys["sys/dev/block/8:1"] = link(usbLink + "sda1")
	fsys["sys/block/sda/removable"] = file("1\n")
	s := NewCollector(fsys, (&statfsAll{}).fn).Collect(t0)
	var stick *Storage
	for i := range s.Storage {
		if s.Storage[i].Device == "sda1" {
			stick = &s.Storage[i]
		}
	}
	if stick == nil || stick.MountPoint != "/media/STICK" || stick.Disk != "sda" || stick.Removable == nil || !*stick.Removable || !stick.USB {
		t.Fatalf("storage = %s", mustJSON(t, s.Storage))
	}
	if strings.Contains(mustJSON(t, s), "alice-secret") {
		t.Error("a user name reached the output")
	}
}

// A network share, an overlay root and memory mounts: shown by kind; a share is
// never asked for its size and its source never reaches the output.
func TestStorageOtherMounts(t *testing.T) {
	fsys := refStorage(t)
	fsys["proc/self/mountinfo"] = file(string(fsys["proc/self/mountinfo"].Data) +
		"50 22 0:50 / /mnt/share rw - nfs4 192.0.2.10:/export/secret-share rw,addr=192.0.2.10\n" +
		"51 22 0:51 / /srv/media rw - cifs //198.51.100.7/films rw\n" +
		"52 22 0:52 / /run/user/1000 rw - tmpfs tmpfs rw\n" +
		"53 22 0:53 / /dev/shm rw - tmpfs tmpfs rw\n" +
		"54 22 0:54 / /var/lib/docker/overlay2/abc/merged rw - overlay overlay rw\n" +
		"55 22 0:55 / /data/scratch rw - ramfs ramfs rw\n" +
		"56 22 0:56 / /mnt/backup rw - nfs 192.0.2.11:/b rw\n")
	st := &statfsAll{}
	s := NewCollector(fsys, st.fn).Collect(t0)
	var got []string
	for _, m := range s.Mounts {
		got = append(got, m.MountPoint+" "+m.Kind+" "+fmt.Sprint(m.TotalBytes != nil))
	}
	want := "/data/scratch memory true,/mnt/backup network false,/mnt/share network false,/srv/media network false,/tmp memory true"
	if strings.Join(got, ",") != want {
		t.Errorf("mounts %v\nwant  %s", got, want)
	}
	for _, c := range st.calls {
		if strings.HasPrefix(c, "/mnt/") || c == "/srv/media" {
			t.Errorf("statfs on the network share %s", c)
		}
	}
	out := mustJSON(t, s)
	for _, leak := range []string{"secret-share", "192.0.2.10", "198.51.100.7", "films", "/export"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q reached the output", leak)
		}
	}

	// An overlay root is listed (its changes may not persist).
	fsys["proc/self/mountinfo"] = file("22 1 0:30 / / rw - overlay overlay rw,lowerdir=/media/root-ro,upperdir=/media/root-rw/overlay\n")
	s = NewCollector(fsys, st.fn).Collect(t0)
	if len(s.Storage) != 0 || len(s.Mounts) != 1 || s.Mounts[0].Kind != MountOverlay || s.Mounts[0].MountPoint != "/" {
		t.Errorf("overlay root: %s %s", mustJSON(t, s.Storage), mustJSON(t, s.Mounts))
	}
}

// The mount table of the service's own namespace (task SF1, fixture
// pi-sandbox): ProtectHome, ProtectKernelModules and PrivateTmp put bind
// mounts of the tmpfs at /run over /home, /root, /usr/lib/modules and files
// under /proc, and the private /tmp over the host's /tmp; StateDirectory,
// CacheDirectory and PrivateTmp bind directories of the root file system.
// Only the device's own mounts are listed: the two partitions, and /tmp once.
func TestStorageServiceSandbox(t *testing.T) {
	fsys := refStorage(t)
	data, err := fs.ReadFile(fixture(t, "pi-sandbox"), "proc/self/mountinfo")
	if err != nil {
		t.Fatal(err)
	}
	fsys["proc/self/mountinfo"] = &fstest.MapFile{Data: data}
	st := &statfsAll{}
	s := NewCollector(fsys, st.fn).Collect(t0)
	var got []string
	for _, f := range s.Storage {
		got = append(got, f.MountPoint+":"+f.Device)
	}
	if strings.Join(got, ",") != "/:mmcblk0p2,/boot/firmware:mmcblk0p1" {
		t.Errorf("storage = %v", got)
	}
	got = nil
	for _, m := range s.Mounts {
		got = append(got, m.MountPoint+":"+m.Kind)
	}
	if strings.Join(got, ",") != "/tmp:memory" {
		t.Errorf("other mounts = %v, want only /tmp", got)
	}
	tmp := 0
	for _, c := range st.calls {
		switch c {
		case "/tmp":
			tmp++
		case "/home", "/root", "/usr/lib/modules", "/var/tmp", "/var/lib/oarfish", "/var/cache/oarfish":
			t.Errorf("statfs on the sandbox mount %s", c)
		}
	}
	if tmp != 1 {
		t.Errorf("statfs on /tmp %d times", tmp)
	}
	if out := mustJSON(t, s); strings.Contains(out, "systemd") || strings.Contains(out, "inaccessible") {
		t.Errorf("a root field reached the output: %s", out)
	}
}

// A bind mount of a real file system is never listed, even when the device
// has no other mount in the table; a btrfs subvolume (root = subvol=) is.
func TestStorageBindMountIsNeverAFileSystem(t *testing.T) {
	fsys := refStorage(t)
	fsys["proc/self/mountinfo"] = file("50 1 179:2 /var/lib/oarfish /var/lib/oarfish rw - ext4 /dev/mmcblk0p2 rw\n" +
		"51 1 179:2 /var/cache/oarfish /var/cache/oarfish rw - ext4 /dev/mmcblk0p2 rw\n" +
		"52 1 179:1 /overlays /srv/firmware rw - vfat /dev/mmcblk0p1 rw\n" +
		"53 1 259:2 /@ / rw - btrfs /dev/nvme0n1p2 rw,subvolid=256,subvol=/@\n" +
		"54 1 259:3 /@/srv/data /srv/data rw - btrfs /dev/nvme0n1p3 rw,subvolid=256,subvol=/@\n" +
		"55 1 259:4 /@data /data rw - btrfs /dev/nvme0n1p4 rw\n")
	st := &statfsAll{}
	s := NewCollector(fsys, st.fn).Collect(t0)
	var got []string
	for _, f := range s.Storage {
		got = append(got, f.MountPoint+":"+f.FSType)
	}
	if strings.Join(got, ",") != "/:btrfs" {
		t.Errorf("storage = %v, want only the btrfs subvolume at /", got)
	}
	if len(st.calls) != 1 || st.calls[0] != "/" {
		t.Errorf("statfs calls = %v", st.calls)
	}
}

// More other mounts than the bound: at most MaxOther, in mount-point order;
// a mount point listed twice is one entry.
func TestStorageOtherMountsBound(t *testing.T) {
	fsys := refStorage(t)
	var b strings.Builder
	b.WriteString(string(fsys["proc/self/mountinfo"].Data))
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "%d 22 0:%d / /mnt/t%02d rw - tmpfs tmpfs rw\n", 100+i, 100+i, i)
	}
	b.WriteString("200 22 0:99 / /tmp rw - tmpfs tmpfs rw\n")
	fsys["proc/self/mountinfo"] = file(b.String())
	s := NewCollector(fsys, (&statfsAll{}).fn).Collect(t0)
	if len(s.Mounts) != MaxOther || s.Mounts[0].MountPoint != "/mnt/t00" || s.Mounts[MaxOther-1].MountPoint != "/mnt/t07" {
		t.Errorf("mounts = %s", mustJSON(t, s.Mounts))
	}
	if MaxOther != 8 {
		t.Error("the bound of ST1 changed")
	}
}

// Sources that are not a kernel block device name show no device: a path by
// UUID or label without a sysfs link, a link that is hostile or not a block
// link, a removable flag that is neither 0 nor 1.
func TestStorageDeviceOnlyFromTheKernelName(t *testing.T) {
	for name, c := range map[string]struct {
		mount string
		links map[string]*fstest.MapFile
		dev   string
	}{
		"by-uuid, no link":  {"22 1 179:9 / /data rw - ext4 /dev/disk/by-uuid/0000-SECRET rw\n", nil, ""},
		"by-label, no link": {"22 1 179:9 / /data rw - ext4 /dev/disk/by-label/MY-DATA rw\n", nil, ""},
		"by-uuid with link": {"22 1 179:9 / /data rw - ext4 /dev/disk/by-uuid/0000-SECRET rw\n",
			map[string]*fstest.MapFile{"sys/dev/block/179:9": link(mmcLink + "mmcblk0p9")}, "mmcblk0p9"},
		"/dev/root with link": {"22 1 179:2 / / rw - ext4 /dev/root rw\n", map[string]*fstest.MapFile{"sys/dev/block/179:2": link(mmcLink + "mmcblk0p2")}, "mmcblk0p2"},
		"hostile link": {"22 1 179:9 / /data rw - ext4 /dev/mmcblk0p9 rw\n",
			map[string]*fstest.MapFile{"sys/dev/block/179:9": link("../../devices/block/mmcblk0/../../etc")}, ""},
		"link with a strange name": {"22 1 179:9 / /data rw - ext4 /dev/mmcblk0p9 rw\n",
			map[string]*fstest.MapFile{"sys/dev/block/179:9": link("../../devices/block/mmc blk0/p9")}, ""},
		"link without block": {"22 1 179:9 / /data rw - ext4 /dev/mmcblk0p9 rw\n",
			map[string]*fstest.MapFile{"sys/dev/block/179:9": link("../../devices/platform/x/mmcblk0p9")}, ""},
		"link too deep": {"22 1 179:9 / /data rw - ext4 /dev/mmcblk0p9 rw\n",
			map[string]*fstest.MapFile{"sys/dev/block/179:9": link("../../devices/block/a/b/c")}, ""},
		"plain /dev/ source, no link": {"22 1 179:9 / /data rw - ext4 /dev/mmcblk0p9 rw\n", nil, ""},
	} {
		fsys := cloneFixture(t, "pi-zero-2w")
		fsys["proc/self/mountinfo"] = file(c.mount)
		for k, v := range c.links {
			fsys[k] = v
		}
		fsys["sys/block/mmcblk0/removable"] = file("maybe\n")
		s := NewCollector(fsys, (&statfsAll{}).fn).Collect(t0)
		if len(s.Storage) != 1 || s.Storage[0].Device != c.dev || s.Storage[0].Removable != nil {
			t.Errorf("%s: %s", name, mustJSON(t, s.Storage))
		}
		for _, leak := range []string{"SECRET", "MY-DATA", "by-uuid", "by-label", "etc"} {
			if strings.Contains(mustJSON(t, s.Storage), leak) {
				t.Errorf("%s: %q reached the output", name, leak)
			}
		}
	}
}

// The snapshot does not share the slow tier's memory.
func TestStorageSnapshotIsACopy(t *testing.T) {
	c := NewCollector(refStorage(t), (&statfsAll{}).fn)
	a := c.Collect(t0)
	*a.Storage[0].Removable = true
	*a.Mounts[0].TotalBytes = 1
	b := c.Collect(t0.Add(2_000_000_000))
	if *b.Storage[0].Removable || *b.Mounts[0].TotalBytes == 1 {
		t.Error("a snapshot changed the cached slow tier")
	}
}
