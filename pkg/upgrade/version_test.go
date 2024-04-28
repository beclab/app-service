package upgrade

import (
	"context"
	"testing"

	"bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
)

func TestLatestVersion(t *testing.T) {
	v, h, err := GetTerminusReleaseVersion(context.Background(), &v1alpha1.Terminus{Spec: v1alpha1.TerminusSpec{
		ReleaseServer: v1alpha1.ReleaseServer{ServerType: GITHUB},
		Version:       "0.3.6"}}, true)
	if err != nil {
		klog.Error(err)
		t.Fail()
	} else {
		t.Log(v.String(), ", ", h.Upgrade.MinVersion)
	}
}
