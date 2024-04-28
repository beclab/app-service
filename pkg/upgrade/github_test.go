package upgrade

import (
	"context"
	"net/http"
	"testing"
)

func TestGithubRelease(t *testing.T) {
	c := http.Client{}

	client := NewGithubRelease(&c, "eball", "install-wizard-public")

	ctx := context.TODO()
	v, e := client.getLatestReleaseVersion(ctx, false)
	if e != nil {
		t.Error(e)
		t.Fail()

		return
	}

	t.Log(v.String())

	u, f, h, e := client.getLatestReleaseBundle(ctx, false)
	if e != nil {
		t.Error(e)
		t.Fail()

		return

	}

	// if f != nil {
	// 	t.Log(f.String())
	// 	p, _ := client.downloadAndUnpack(ctx, f)
	// 	t.Log(p)
	// }

	if u != nil {
		t.Log(u.String())
	}

	if h != nil {
		t.Log(f.String())
		p, _ := client.readVersionHint(ctx, h)
		if p != nil {
			t.Log(p.Upgrade.MinVersion)
		}
	}
}
