package routine

import "testing"

func TestAssessRoutineOutputLadder(t *testing.T) {
	tests := []struct {
		name         string
		output       string
		reportExists bool
		wantOK       bool
		wantReason   string
	}{
		{
			name:         "complete with report succeeds",
			output:       "did the work\nROUTINE_COMPLETE\n",
			reportExists: true,
			wantOK:       true,
		},
		{
			name:         "complete glued to prose still counts",
			output:       "all done ROUTINE_COMPLETEthanks",
			reportExists: true,
			wantOK:       true,
		},
		{
			name:         "failed sentinel reason becomes fail reason",
			output:       "tried hard\nROUTINE_FAILED: jira api returned 500\n",
			reportExists: true,
			wantReason:   "jira api returned 500",
		},
		{
			name:         "failed sentinel wins even with report and complete text",
			output:       "ROUTINE_COMPLETE was my goal but\nROUTINE_FAILED: could not reach source",
			reportExists: true,
			wantReason:   "could not reach source",
		},
		{
			name:         "failed sentinel without reason",
			output:       "ROUTINE_FAILED:",
			reportExists: true,
			wantReason:   "agent reported failure",
		},
		{
			name:         "clean exit no sentinel",
			output:       "I finished the analysis and wrote everything up.",
			reportExists: true,
			wantReason:   "missing ROUTINE_COMPLETE sentinel",
		},
		{
			name:         "sentinel but no report",
			output:       "ROUTINE_COMPLETE",
			reportExists: false,
			wantReason:   "missing report",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := assessRoutineOutput(tt.output, tt.reportExists)
			if got.Succeeded != tt.wantOK {
				t.Fatalf("Succeeded = %v, want %v (reason %q)", got.Succeeded, tt.wantOK, got.FailReason)
			}
			if got.FailReason != tt.wantReason {
				t.Fatalf("FailReason = %q, want %q", got.FailReason, tt.wantReason)
			}
		})
	}
}
