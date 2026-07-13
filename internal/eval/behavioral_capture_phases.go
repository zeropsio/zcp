package eval

import (
	"context"
	"fmt"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
)

func (r *Runner) runBehavioralUserSim(ctx context.Context, sc *Scenario, suiteID, sessionID, transcriptFile string, result *BehavioralResult) error {
	loopTimeout := defaultUserSimStageTimeout
	if sc.UserSim != nil && sc.UserSim.StageTimeoutSeconds > 0 {
		loopTimeout = time.Duration(sc.UserSim.StageTimeoutSeconds) * time.Second
	}
	loopCtx, cancelLoop := context.WithTimeout(ctx, loopTimeout)
	defer cancelLoop()
	simRunner := r.captureUserSimRunner(sc, suiteID, sc.ID, r.userSimRunner(sc))
	resumeIteration := 0
	observedResume := func(resumeCtx context.Context, resumeSessionID, userMessage, file string) error {
		resumeIteration++
		phase := fmt.Sprintf("agent.resume.%d", resumeIteration)
		invocationID := sc.ID + "/" + phase
		invocation := r.captureInvocationStart(resumeCtx, suiteID, sc.ID, invocationID, phase, resumeSessionID)
		resumeErr := r.spawnClaudeResumeAppend(resumeCtx, resumeSessionID, userMessage, file, captureProcessScope{
			evalRunID: suiteID, scenarioRunID: sc.ID, invocationID: invocationID, phase: phase,
		})
		status := capture.CaptureComplete
		if resumeErr != nil {
			status = capture.CapturePartial
		}
		invocation.End(context.Background(), status, resumeErr)
		return resumeErr
	}
	return runUserSimLoop(loopCtx, sc, sessionID, transcriptFile, simRunner, observedResume, ClassifyTranscriptTail, result)
}
