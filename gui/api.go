// api.go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/ollama/ollama/api"
)

// Downloads a model from the Ollama API.
func downloadModel(model api.ListModelResponse) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	req := &api.PullRequest{
		Model: model.Model,
	}
	progressFunc := func(resp api.ProgressResponse) error {
		fmt.Printf("Progress: status=%v, total=%v, completed=%v\n", resp.Status, resp.Total, resp.Completed)
		return nil
	}

	err = client.Pull(ctx, req, progressFunc)
	if err != nil {
		log.Fatal(err)
	}
}

// Lists all models available locally.
func listAllModels() {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	models, err := client.List(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Available Models:")
	for _, model := range models.Models {
		log.Printf("- %s\n", model.Model)
	}
}

// Deletes a specified model by name.
func deleteModel(modelName string) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	req := &api.DeleteRequest{
		Model: modelName,
	}

	err = client.Delete(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Model '%s' deleted successfully.\n", modelName)
}

// Gets detailed information about a specified model.
func getModel(modelName string) {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	req := &api.ShowRequest{
		Model: modelName,
	}

	modelInfo, err := client.Show(ctx, req)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Model: %s\nDescription: %s\nLicense: %s\n", modelInfo.ModelInfo, modelInfo.Details, modelInfo.License)
}

// Lists all running models.
func getAllModels() {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	runningModels, err := client.ListRunning(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Running Models:")
	for _, model := range runningModels.Models {
		log.Printf("- %s\n", model.Model)
	}
}
