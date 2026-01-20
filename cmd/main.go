package main

import (
	"log"
	"os"

	"sync"
	"time"

	"github.com/furmanp/gitlab-activity-importer/internal"
	"github.com/furmanp/gitlab-activity-importer/internal/services"
)

func main() {
	startNow := time.Now()
	err := internal.SetupEnv()
	if err != nil {
		log.Fatalf("Error during loading environmental variables: %v", err)
	}

	gitlabUser, err := services.GetGitlabUser()

	if err != nil {
		log.Fatalf("Error during reading GitLab User data: %v", err)
	}

	gitLabUserID := gitlabUser.ID

	projectIds, err := services.GetUsersProjectsIds(gitLabUserID)

	if err != nil {
		log.Fatalf("Error during getting users projects: %v", err)
	}
	if len(projectIds) == 0 {
		log.Print("No contributions found for this user. Closing the program.")
		return
	}

	log.Printf("Found contributions in %d projects", len(projectIds))

	repo := services.OpenOrInitClone()

	err = services.PullLatestChanges(repo)
	if err != nil {
		log.Fatalf("Error pulling latest changes: %v", err)
	}

	commitChannel := make(chan []internal.Commit)

	var fetchWG sync.WaitGroup
	fetchWG.Add(1)
	go func() {
		defer fetchWG.Done()
		services.FetchGitLabCommits(projectIds, os.Getenv("GITLAB_USERNAME"), commitChannel)
	}()

	if internal.IsCodebergSyncEnabled() {
		fetchWG.Add(1)
		go func() {
			defer fetchWG.Done()
			services.FetchCodebergCommits(os.Getenv("CODEBERG_USERNAME"), commitChannel)
		}()
	}

	go func() {
		fetchWG.Wait()
		close(commitChannel)
	}()

	var commitWG sync.WaitGroup
	commitWG.Add(1)

	var totalCommitsCreated int
	go func() {
		defer commitWG.Done()
		totalCommits := 0
		for commits := range commitChannel {
			if localCommits, err := services.CreateLocalCommit(repo, commits); err == nil {
				totalCommits += localCommits
			} else {
				log.Printf("Error creating local commit: %v", err)
			}
		}
		totalCommitsCreated = totalCommits
		log.Printf("Imported %v commits.\n", totalCommits)
	}()

	commitWG.Wait()

	if totalCommitsCreated > 0 {
		if err := services.PushLocalCommits(repo); err != nil {
			log.Fatalf("Error pushing local commits: %v", err)
			return
		}
		log.Println("Successfully pushed commits to remote repository.")
	} else {
		log.Println("No new commits were created, skipping push operation.")
	}
	log.Printf("Operation took: %v in total.", time.Since(startNow))
}
