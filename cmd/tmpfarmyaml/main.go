// Throwaway (PROVE S1b tracer): render the run-project import YAML from env.
// Never committed; deleted after the run.
package main

import (
	"fmt"
	"os"

	"github.com/zeropsio/zcp/internal/eval/farm"
)

func main() {
	d := farm.RunDescriptor{
		BatchID:         os.Getenv("T_BATCH"),
		RunID:           os.Getenv("T_RUN"),
		ScenarioID:      os.Getenv("T_SCENARIO"),
		EvaluatorSHA256: os.Getenv("T_EVAL_SHA"),
		CandidateSHA256: os.Getenv("T_CAND_SHA"),
		Sink: farm.Sink{URL: os.Getenv("ZCP_FARM_S3_URL"), Bucket: os.Getenv("ZCP_FARM_S3_BUCKET"),
			Key: os.Getenv("ZCP_FARM_S3_KEY"), Secret: os.Getenv("ZCP_FARM_S3_SECRET")},
		Credentials: []farm.Credential{{Mode: farm.CredentialOAuthToken, Value: os.Getenv("CLAUDE_CODE_OAUTH_TOKEN")}},
	}
	y, err := farm.ImportYAML(d)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Stdout.Write(y)
}
