/*
Copyright 2026 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package probe

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func mangleMountInfoPath(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case ' ', '\t', '\n', '\\':
			fmt.Fprintf(&b, `\%03o`, c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

func FuzzUnescapeMountInfo(f *testing.F) {
	for _, seed := range []string{
		`/plain`,
		`/a\040b`,
		`/tab\011newline\012`,
		`/back\134slash`,
		`/short\04`,
		`/not\999octal`,
		`\`,
		`\\134`,
		`\400\777`,
		`/tmp/root\040squash\134probe`,
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		got := unescapeMountInfo(s)
		if len(got) > len(s) {
			t.Fatalf("unescapeMountInfo(%q) = %q grew the string", s, got)
		}
		if !strings.Contains(s, `\`) && got != s {
			t.Fatalf("unescapeMountInfo(%q) = %q changed a string without escapes", s, got)
		}
		if mangled := mangleMountInfoPath(s); unescapeMountInfo(mangled) != s {
			t.Fatalf("unescapeMountInfo(%q) = %q, want the path %q the kernel mangled", mangled, unescapeMountInfo(mangled), s)
		}
	})
}

func FuzzIsMountPoint(f *testing.F) {
	f.Add("/tmp/root-squash-probe-1", "/tmp/root squash\\probe", "/tmp/root-squash-probe-2")
	f.Add("/", "/tmp", "/tmp/")
	f.Add("/mnt/a\tb", "/mnt/a\nb", "/mnt/a b")
	f.Add("/srv/nfs/pvc-1", "/srv/nfs/pvc-1/../pvc-2", "/srv/nfs/pvc-10")

	f.Fuzz(func(t *testing.T, mounted, other, probed string) {
		if !strings.HasPrefix(mounted, "/") || !strings.HasPrefix(other, "/") || !strings.HasPrefix(probed, "/") {
			return
		}
		mounted, other, probed = filepath.Clean(mounted), filepath.Clean(other), filepath.Clean(probed)

		table := "22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw\n" +
			fmt.Sprintf("610 22 0:52 / %s rw,relatime - nfs4 nfs.example.com:/srv/share rw,vers=4.2\n", mangleMountInfoPath(mounted)) +
			fmt.Sprintf("611 22 0:53 /sub %s rw,nosuid shared:7 master:3 - nfs nfs.example.com:/srv/other rw,vers=3\n", mangleMountInfoPath(other))

		for _, target := range []string{mounted, other} {
			got, err := isMountPoint(strings.NewReader(table), target)
			if err != nil || !got {
				t.Fatalf("isMountPoint(%q) = %t, %v; want true for the table\n%s", target, got, err, table)
			}
		}

		want := probed == mounted || probed == other || probed == "/"
		got, err := isMountPoint(strings.NewReader(table), probed)
		if err != nil || got != want {
			t.Fatalf("isMountPoint(%q) = %t, %v; want %t for the table\n%s", probed, got, err, want, table)
		}
	})
}
