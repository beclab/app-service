package upgrade

import "k8s.io/klog/v2"

type PipelineState int

func (s PipelineState) Cancelable() bool {
	return s == Running
}

func (s PipelineState) String() string {
	switch s {
	case Canceled:
		return "canceled"
	case New:
		return "new"
	case Running:
		return "running"
	case Done:
		return "done"
	case Failed:
		return "failed"
	}

	return "unknown"
}

type Job interface {
	Run(ctx *PipelineContext) error
	Cancel()
}

type Pipeline struct {
	Name string
	Jobs []Job
	Ctx  *PipelineContext

	State PipelineState

	Error error

	runningJob Job
}

func NewPipeline(name string, jobs []Job, ctx *PipelineContext) *Pipeline {
	return &Pipeline{
		Name:  name,
		Jobs:  jobs,
		Ctx:   ctx,
		State: New,
	}
}

func (p *Pipeline) Execute() error {
	p.State = Running

	for _, j := range p.Jobs {
		p.runningJob = j
		err := j.Run(p.Ctx)
		if err != nil {
			p.State = Failed
			p.Error = err
			return err
		}
		if p.State == Canceled {
			klog.Infof("Pipeline name=%s canceled", p.Name)
			return nil
		}
	}

	p.State = Done
	klog.Infof("Pipeline name=%s finished", p.Name)
	return nil
}

func (p *Pipeline) Cancel() {
	p.State = Canceled
	p.runningJob.Cancel()
	p.Ctx.CancelInstall()
}
