package bot

import (
	"fmt"
	"github.com/bradleyfalzon/ghinstallation"
	"github.com/google/go-github/github"
	"golang.org/x/net/context"
	"net/http"
)

const ApprovedReviewsBeforeReadyToMerge = 2

type GithubApp struct {
	client        *github.Client
	webhookSecret []byte
	integrationId int
	privateKey    []byte
}

func New(integrationId int, webhookSecret []byte, privateKey []byte) *GithubApp {
	return &GithubApp{
		client:        github.NewClient(nil),
		webhookSecret: webhookSecret,
		integrationId: integrationId,
		privateKey:    privateKey,
	}
}

func (s *GithubApp) getClientForInstallation(installationId int) (*github.Client, error) {
	itr, err := ghinstallation.New(
		http.DefaultTransport,
		s.integrationId,
		installationId,
		s.privateKey,
	)
	if err != nil {
		return nil, err
	}

	return github.NewClient(&http.Client{Transport: itr}), nil
}

func (s *GithubApp) setupLabelsForAllRepositories(event *github.InstallationEvent) {
	ghi, err := s.getClientForInstallation(int(*event.Installation.ID))
	if err != nil {
		fmt.Printf("Cannot get client for installation %d\n", *event.Installation.ID)

		return
	}

	opt := &github.RepositoryListByOrgOptions{
		Type:        "all",
		ListOptions: github.ListOptions{Page: 1, PerPage: 50},
	}
	for {
		fmt.Printf("Listing repositories page %d\n", opt.Page)
		repositories, resp, err := ghi.Repositories.ListByOrg(context.Background(), *event.Installation.Account.Login, opt)
		if err != nil {
			fmt.Printf("Could not list repositories for %s: %s\n", *event.Installation.Account.Login, err)

			return
		}

		for _, r := range repositories {
			if *r.Archived == true {
				continue
			}

			err := s.createLabels(int(*event.Installation.ID), *event.Installation.Account.Login, *r.Name)
			if err != nil {
				fmt.Printf("Could not create labels for repository %s: %s\n", *r.URL, err)

				return
			}
		}

		if resp.NextPage == 0 {
			break
		}

		opt.Page = resp.NextPage
	}
}

func (s *GithubApp) handlePullRequestCreated(event *github.PullRequestEvent) error {
	//ghi, err := s.getClientForInstallation(int(*event.Installation.ID))
	//if err != nil {
	//	return err
	//}

	//if len(event.PullRequest.Labels) == 0 {
	//	_, _, err = ghi.Issues.AddLabelsToIssue(
	//		context.Background(),
	//		*event.Repo.Owner.Login,
	//		*event.Repo.Name,
	//		*event.PullRequest.Number,
	//		[]string{"work in progress"},
	//	)
	//	if err != nil {
	//		fmt.Printf("Could not add label %s to PR %s\n", "work in progress", *event.PullRequest.URL)
	//		return err
	//	}
	//}

	return nil
}

func (s *GithubApp) applyPullRequestLabels(installationId int, org, repo string, number int, url string) error {
	ghi, err := s.getClientForInstallation(installationId)
	if err != nil {
		return err
	}

	reviews, _, err := ghi.PullRequests.ListReviews(context.Background(), org, repo, number, &github.ListOptions{})
	if err != nil {
		return err
	}

	approvedReviews := 0
	for _, r := range reviews {
		if *r.State == "APPROVED" {
			approvedReviews++
		}
	}

	fmt.Printf("PR %d received %d reviews\n", number, approvedReviews)

	if approvedReviews == 1 {
		_, _, err = ghi.Issues.AddLabelsToIssue(
			context.Background(),
			org,
			repo,
			number,
			[]string{"first approval"},
		)
		if err != nil {
			fmt.Printf("Could not add label %s to PR %s\n", "first approval", url)
			return err
		}

		_, err = ghi.Issues.RemoveLabelForIssue(
			context.Background(),
			org,
			repo,
			number,
			"ready to merge",
		)
		if err != nil {
			fmt.Printf("Could not remove label %s to PR %s, it probably doesn't exist\n", "ready to merge", url)
		}
	} else if approvedReviews >= ApprovedReviewsBeforeReadyToMerge {
		_, _, err = ghi.Issues.AddLabelsToIssue(
			context.Background(),
			org,
			repo,
			number,
			[]string{"ready to merge"},
		)
		if err != nil {
			fmt.Printf("Could not add label %s to PR %s\n", "ready to merge", url)
			return err
		}

		_, err = ghi.Issues.RemoveLabelForIssue(
			context.Background(),
			org,
			repo,
			number,
			"ready for review",
		)
		if err != nil {
			fmt.Printf("Could not remove label %s to PR %s, it probably doesn't exist\n", "ready for review", url)
		}

		_, err = ghi.Issues.RemoveLabelForIssue(
			context.Background(),
			org,
			repo,
			number,
			"first approval",
		)
		if err != nil {
			fmt.Printf("Could not remove label %s to PR %s, it probably doesn't exist\n", "first approval", url)
		}
	} else {
		_, err = ghi.Issues.RemoveLabelForIssue(
			context.Background(),
			org,
			repo,
			number,
			"ready to merge",
		)
		if err != nil {
			fmt.Printf("Could not remove label %s to PR %s, it probably doesn't exist\n", "ready to merge", url)
		}

		_, err = ghi.Issues.RemoveLabelForIssue(
			context.Background(),
			org,
			repo,
			number,
			"first approval",
		)
		if err != nil {
			fmt.Printf("Could not remove label %s to PR %s, it probably doesn't exist\n", "first approval", url)
		}
	}

	return nil
}

func (s *GithubApp) handleRepositoryCreatedEvent(event *github.RepositoryEvent) error {
	err := s.createLabels(int(*event.Installation.ID), *event.Org.Login, *event.Repo.Name)
	if err != nil {
		return err
	}

	return nil
}

func (s *GithubApp) createLabels(installationId int, owner, repo string) error {
	ghi, err := s.getClientForInstallation(installationId)
	if err != nil {
		return err
	}

	labels, _, err := ghi.Issues.ListLabels(context.Background(), owner, repo, &github.ListOptions{})
	if err != nil {
		return err
	}

	labelsMap := make(map[string]*github.Label, len(labels))
	for _, l := range labels {
		labelsMap[*l.Name] = l

		if owner == "TicketSwap" {
			if *l.Name == "bug" || *l.Name == "duplicate" || *l.Name == "enhancement" || *l.Name == "good first issue" || *l.Name == "help wanted" || *l.Name == "invalid" || *l.Name == "question" || *l.Name == "wontfix" {
				fmt.Printf("Deleting default label %s...\n", *l.Name)

				_, err = ghi.Issues.DeleteLabel(context.Background(), owner, repo, *l.Name)
				if err != nil {
					fmt.Printf("Could not delete label %s: %s\n", *l.Name, err)
					return err
				}
			}
		}
	}

	labelTemplate := map[string]string{
		"work in progress": "0052cc",
		"first approval":   "bfe5bf",
		"ready for review": "fef2c0",
		"ready to merge":   "0e8a16",
	}
	for labelName, labelColor := range labelTemplate {
		label := labelsMap[labelName]

		if label != nil && *label.Color != labelColor {
			fmt.Printf("Label %s exists but has color %s instead of %s, changing...\n", labelName, *label.Color, labelColor)

			label.Color = github.String(labelColor)

			_, _, err = ghi.Issues.EditLabel(context.Background(), owner, repo, labelName, label)
			if err != nil {
				fmt.Printf("Could not edit label %s: %s\n", labelName, err)
				return err
			}
		} else if label == nil {
			fmt.Printf("Label %s does not exist, creating...\n", labelName)
			label = &github.Label{
				Name:  github.String(labelName),
				Color: github.String(labelColor),
			}

			_, _, err = ghi.Issues.CreateLabel(context.Background(), owner, repo, label)
			if err != nil {
				fmt.Printf("Could not create label %s: %s\n", *label.Name, err)
				return err
			}
		}
	}

	return nil
}

func (s *GithubApp) HandlerFunc(w http.ResponseWriter, r *http.Request) {
	payload, err := github.ValidatePayload(r, s.webhookSecret)
	if err != nil {
		fmt.Printf("Invalid signature\n")

		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Invalid signature"))

		return
	}

	event, err := github.ParseWebHook(github.WebHookType(r), payload)
	if err != nil {
		fmt.Printf("Cannot parse webhook payload\n")

		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Cannot parse webhook payload"))

		return
	}

	switch event := event.(type) {
	case *github.InstallationEvent:
		fmt.Printf("Installation %s for %s\n", *event.Action, *event.Installation.Account.Login)

		if *event.Action == "created" {
			go s.setupLabelsForAllRepositories(event)
		}
	case *github.PullRequestReviewEvent:
		s.applyPullRequestLabels(
			int(*event.Installation.ID),
			*event.Organization.Login,
			*event.Repo.Name,
			*event.PullRequest.Number,
			*event.PullRequest.URL,
		)
	case *github.PullRequestEvent:
		if *event.Action == "opened" {
			s.handlePullRequestCreated(event)
		} else if *event.Action == "labeled" {
			s.applyPullRequestLabels(
				int(*event.Installation.ID),
				*event.Repo.Owner.Login,
				*event.Repo.Name,
				*event.PullRequest.Number,
				*event.PullRequest.URL,
			)
		}
	case *github.RepositoryEvent:
		if *event.Action == "created" {
			err = s.handleRepositoryCreatedEvent(event)
			if err != nil {
				fmt.Printf("Cannot handle repository created event: %s\n", err)

				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Cannot handle repository created event"))

				return
			}
		}
	default:
		fmt.Printf("Skipping event %+v\n", event)
	}

	w.WriteHeader(http.StatusOK)
}