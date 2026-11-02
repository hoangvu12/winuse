package win

import "testing"

func TestFormatWindowsBuild(t *testing.T) {
	cases := []struct {
		major, minor, build, ubr uint32
		display, want            string
	}{
		{10, 0, 26200, 6899, "25H2", "Windows 11 25H2, build 26200.6899"},
		{10, 0, 19045, 4529, "22H2", "Windows 10 22H2, build 19045.4529"},
		{10, 0, 22000, 0, "", "Windows 11, build 22000"},
		{6, 3, 9600, 0, "", "Windows 6.3, build 9600"},
	}
	for _, c := range cases {
		if got := FormatWindowsBuild(c.major, c.minor, c.build, c.ubr, c.display); got != c.want {
			t.Errorf("FormatWindowsBuild(%d, %d, %d, %d, %q) = %q, want %q", c.major, c.minor, c.build, c.ubr, c.display, got, c.want)
		}
	}
}
