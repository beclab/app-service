package v1alpha1

type ImageManagerState string

const (
	ImDownloading ImageManagerState = "downloading"
	ImFailed      ImageManagerState = "failed"
	ImCanceled    ImageManagerState = "downloadingCanceled"
	ImCompleted   ImageManagerState = "completed"
)

func (i ImageManagerState) String() string {
	return string(i)
}
