package notes

import "testing"

func TestClassifyWorksOutSiteKeys(t *testing.T) {
	cases := []struct{ target, key string }{
		// Subdomains go to the registrable domain, whatever the scheme,
		// path, port or case.
		{"https://gist.github.com/someone/abc", "github.com"},
		{"gist.github.com", "github.com"},
		{"GitHub.com", "github.com"},
		{"https://WWW.Example.COM:8443/a?b=c#d", "example.com"},
		{"example.com/some/page", "example.com"},
		{"example.com.", "example.com"},
		// Multi-label public suffixes, ICANN and private.
		{"https://news.bbc.co.uk/x", "bbc.co.uk"},
		{"someone.github.io", "someone.github.io"},
		{"https://my-app.vercel.app/", "my-app.vercel.app"},
		// A host that is itself a public suffix keys as itself.
		{"https://github.io/", "github.io"},
		// localhost and IP addresses keep their port.
		{"http://localhost:3000/login", "localhost:3000"},
		{"localhost:5173", "localhost:5173"},
		{"LOCALHOST", "localhost"},
		{"http://localhost/", "localhost"},
		{"http://localhost:80/", "localhost"}, // the scheme's default port is no port
		{"https://localhost:443/", "localhost"},
		{"http://app.localhost:8080/", "app.localhost:8080"},
		{"http://127.0.0.1:8080/x", "127.0.0.1:8080"},
		{"192.168.1.20:9000", "192.168.1.20:9000"},
		{"10.0.0.1", "10.0.0.1"},
		{"http://[::1]:3000/", "[::1]:3000"},
		{"http://[::1]/", "[::1]"},
		// A one-label host behind a scheme, or with a port, is a site too;
		// only localhost and IP addresses keep the port.
		{"http://intranet/wiki", "intranet"},
		{"devbox:8080", "devbox"},
		{"https://example.com:8443/", "example.com"},
		// Punycode, as browsers report a Unicode host, and a Unicode
		// subdomain typed by hand.
		{"https://shop.xn--bcher-kva.de/", "xn--bcher-kva.de"},
		{"https://bücher.example.de/", "example.de"},
	}
	for _, c := range cases {
		got := Classify(c.target)
		if got.Kind != Site || got.Key != c.key {
			t.Errorf("Classify(%q) = %+v, want site %q", c.target, got, c.key)
		}
	}
}

func TestClassifyWorksOutAppKeys(t *testing.T) {
	cases := []struct{ target, key string }{
		{"excel.exe", "excel.exe"},
		{"EXCEL.EXE", "excel.exe"},
		{`C:\Program Files\Microsoft Office\root\Office16\EXCEL.EXE`, "excel.exe"},
		{`"C:\Program Files\Notepad++\notepad++.exe"`, "notepad++.exe"},
		{"C:/Tools/My App.exe", "my app.exe"},
	}
	for _, c := range cases {
		got := Classify(c.target)
		if got.Kind != App || got.Key != c.key {
			t.Errorf("Classify(%q) = %+v, want app %q", c.target, got, c.key)
		}
	}
}

func TestClassifyRecognizesWindowIDsAndTabTargets(t *testing.T) {
	got := Classify("4242")
	if got.Kind != Window || got.Window != "4242" || got.Tab != "" {
		t.Errorf("Classify(4242) = %+v, want Window 4242", got)
	}
	got = Classify(" 4242:1a2b3c4d ")
	if got.Kind != Window || got.Window != "4242" || got.Tab != "1a2b3c4d" {
		t.Errorf("Classify(4242:1a2b3c4d) = %+v, want Window 4242 tab 1a2b3c4d", got)
	}
}

func TestClassifyRefusesPagesThatAreNoSite(t *testing.T) {
	for _, target := range []string{
		"file:///C:/Users/me/report.html",
		"chrome://settings/",
		"edge://flags",
		"about:blank",
	} {
		got := Classify(target)
		if got.Kind != NoSite || got.Key != "" {
			t.Errorf("Classify(%q) = %+v, want NoSite", target, got)
		}
	}
}

func TestClassifyRefusesFreeFormNames(t *testing.T) {
	for _, target := range []string{"Microsoft Excel", "excel", "Notepad", "readme.txt", "my notes", ""} {
		if got := Classify(target); got.Kind != FreeForm {
			t.Errorf("Classify(%q) = %+v, want FreeForm", target, got)
		}
	}
}

func TestSiteKeyOfPageURLs(t *testing.T) {
	cases := []struct {
		url, key string
		ok       bool
	}{
		{"https://mail.google.com/mail/u/0/#inbox", "google.com", true},
		{"http://localhost:8000/", "localhost:8000", true},
		{"chrome://newtab/", "", false},
		{"file:///C:/x.html", "", false},
		{"about:blank", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		key, ok := SiteKey(c.url)
		if key != c.key || ok != c.ok {
			t.Errorf("SiteKey(%q) = %q, %v; want %q, %v", c.url, key, ok, c.key, c.ok)
		}
	}
}

func TestAppKeyIsTheLowerCasedExeName(t *testing.T) {
	if got := AppKey(`C:\Program Files\Google\Chrome\Application\Chrome.EXE`); got != "chrome.exe" {
		t.Errorf("AppKey = %q, want chrome.exe", got)
	}
	if got := AppKey(""); got != "" {
		t.Errorf("AppKey(\"\") = %q, want empty", got)
	}
}
