package view

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/cli/cli/internal/ghrepo"
	"github.com/cli/cli/pkg/cmd/run/shared"
	"github.com/cli/cli/pkg/cmdutil"
	"github.com/cli/cli/pkg/httpmock"
	"github.com/cli/cli/pkg/iostreams"
	"github.com/cli/cli/pkg/prompt"
	"github.com/google/shlex"
	"github.com/stretchr/testify/assert"
)

func TestNewCmdView(t *testing.T) {
	tests := []struct {
		name     string
		cli      string
		tty      bool
		wants    ViewOptions
		wantsErr bool
	}{
		{
			name:     "blank nontty",
			wantsErr: true,
		},
		{
			name: "blank tty",
			tty:  true,
			wants: ViewOptions{
				Prompt:       true,
				ShowProgress: true,
			},
		},
		{
			name: "exit status",
			cli:  "-e 1234",
			wants: ViewOptions{
				RunID:      "1234",
				ExitStatus: true,
			},
		},
		{
			name: "verbosity",
			cli:  "-v",
			tty:  true,
			wants: ViewOptions{
				Verbose:      true,
				Prompt:       true,
				ShowProgress: true,
			},
		},
		{
			name: "with arg nontty",
			cli:  "1234",
			wants: ViewOptions{
				RunID: "1234",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			io, _, _, _ := iostreams.Test()
			io.SetStdinTTY(tt.tty)
			io.SetStdoutTTY(tt.tty)

			f := &cmdutil.Factory{
				IOStreams: io,
			}

			argv, err := shlex.Split(tt.cli)
			assert.NoError(t, err)

			var gotOpts *ViewOptions
			cmd := NewCmdView(f, func(opts *ViewOptions) error {
				gotOpts = opts
				return nil
			})
			cmd.SetArgs(argv)
			cmd.SetIn(&bytes.Buffer{})
			cmd.SetOut(ioutil.Discard)
			cmd.SetErr(ioutil.Discard)

			_, err = cmd.ExecuteC()
			if tt.wantsErr {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			assert.Equal(t, tt.wants.RunID, gotOpts.RunID)
			assert.Equal(t, tt.wants.ShowProgress, gotOpts.ShowProgress)
			assert.Equal(t, tt.wants.Prompt, gotOpts.Prompt)
			assert.Equal(t, tt.wants.ExitStatus, gotOpts.ExitStatus)
			assert.Equal(t, tt.wants.Verbose, gotOpts.Verbose)
		})
	}
}

func TestViewRun(t *testing.T) {
	created, _ := time.Parse("2006-01-02 15:04:05", "2021-02-23 04:51:00")
	updated, _ := time.Parse("2006-01-02 15:04:05", "2021-02-23 04:55:34")
	testRun := func(name string, id int, s shared.Status, c shared.Conclusion) shared.Run {
		return shared.Run{
			Name:       name,
			ID:         id,
			CreatedAt:  created,
			UpdatedAt:  updated,
			Status:     s,
			Conclusion: c,
			Event:      "push",
			HeadBranch: "trunk",
			JobsURL:    fmt.Sprintf("/runs/%d/jobs", id),
			HeadCommit: shared.Commit{
				Message: "cool commit",
			},
			HeadSha: "1234567890",
			URL:     fmt.Sprintf("runs/%d", id),
		}
	}

	successfulRun := testRun("successful", 3, shared.Completed, shared.Success)

	runs := []shared.Run{
		testRun("timed out", 3, shared.Completed, shared.TimedOut),
		testRun("in progress", 2, shared.InProgress, ""),
		successfulRun,
		testRun("cancelled", 4, shared.Completed, shared.Cancelled),
		testRun("failed", 5, shared.Completed, shared.Failure),
		testRun("neutral", 6, shared.Completed, shared.Neutral),
		testRun("skipped", 7, shared.Completed, shared.Skipped),
		testRun("requested", 8, shared.Requested, ""),
		testRun("queued", 9, shared.Queued, ""),
		testRun("stale", 10, shared.Completed, shared.Stale),
	}

	tests := []struct {
		name     string
		stubs    func(*httpmock.Registry)
		askStubs func(*prompt.AskStubber)
		opts     *ViewOptions
		tty      bool
		wantErr  bool
		wantOut  string
	}{
		// TODO found matching PR
		// TODO did not find matching PR
		// TODO failed job
		// TODO no failed job
		// TODO verbose
		// TODO exit status
		// TODO pass in a run id
		{
			name: "prompts for choice",
			tty:  true,
			stubs: func(reg *httpmock.Registry) {
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs"),
					httpmock.JSONResponse(shared.RunsPayload{
						WorkflowRuns: runs,
					}))
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/actions/runs/3"),
					httpmock.JSONResponse(successfulRun))
				reg.Register(
					httpmock.GraphQL(`query PullRequestForRun`),
					httpmock.StringResponse(``))
				reg.Register(
					httpmock.REST("GET", "runs/3/jobs"),
					httpmock.JSONResponse(shared.JobsPayload{
						Jobs: []shared.Job{
							{
								ID:          10,
								Status:      shared.Completed,
								Conclusion:  shared.Success,
								Name:        "cool job",
								StartedAt:   created,
								CompletedAt: updated,
							},
						},
					}))
				reg.Register(
					httpmock.REST("GET", "repos/OWNER/REPO/check-runs/10/annotations"),
					httpmock.JSONResponse([]shared.Annotation{}))
			},
			askStubs: func(as *prompt.AskStubber) {
				as.StubOne(2)
			},
			opts: &ViewOptions{
				Prompt:       true,
				ShowProgress: true,
			},
			wantOut: "\n✓ trunk successful · 3\nTriggered via push about 59 minutes ago\n\nJOBS\n✓ cool job (ID 10)\n\nFor more information about a job, try: gh job view <job-id>\nview this run on GitHub: runs/3\n",
		},
	}

	for _, tt := range tests {
		reg := &httpmock.Registry{}
		tt.stubs(reg)
		tt.opts.HttpClient = func() (*http.Client, error) {
			return &http.Client{Transport: reg}, nil
		}

		tt.opts.Now = func() time.Time {
			notnow, _ := time.Parse("2006-01-02 15:04:05", "2021-02-23 05:50:00")
			return notnow
		}

		io, _, stdout, _ := iostreams.Test()
		io.SetStdoutTTY(tt.tty)
		tt.opts.IO = io
		tt.opts.BaseRepo = func() (ghrepo.Interface, error) {
			return ghrepo.FromFullName("OWNER/REPO")
		}

		as, teardown := prompt.InitAskStubber()
		defer teardown()
		if tt.askStubs != nil {
			tt.askStubs(as)
		}

		t.Run(tt.name, func(t *testing.T) {
			err := runView(tt.opts)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantOut, stdout.String())
			reg.Verify(t)
		})
	}
}
