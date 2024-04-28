package upgrade

const (
	// GITHUB release server type
	GITHUB = "github"

	// Canceled the pipeline has been canceled.
	Canceled PipelineState = -1
	// New the pipeline has been created.
	New PipelineState = 1
	// Running the pipeline is running.
	Running PipelineState = 2
	// Done the pipeline is completed.
	Done PipelineState = 3
	// Failed the pipeline is failed.
	Failed PipelineState = 4

	tgzPackageSuffix = ".tar.gz"
)
