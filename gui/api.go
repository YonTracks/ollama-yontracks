// api.go
package main

import (
	"context"
	"fmt"

	"github.com/ollama/ollama/api"
)

// downloadModel downloads a model from the Ollama API.
func downloadModel(model api.ListModelResponse) error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	ctx := context.Background()
	req := &api.PullRequest{
		Model: model.Model,
	}
	progressFunc := func(resp api.ProgressResponse) error {
		fmt.Printf("Progress: status=%v, total=%v, completed=%v\n", resp.Status, resp.Total, resp.Completed)
		return nil
	}

	if err := client.Pull(ctx, req, progressFunc); err != nil {
		return fmt.Errorf("error pulling model: %w", err)
	}
	return nil
}

// listAllModels logs all models available locally.
func listAllModels() error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	ctx := context.Background()
	models, err := client.List(ctx)
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	fmt.Println("Available Models:")
	for _, model := range models.Models {
		fmt.Printf("- %s\n", model.Model)
	}
	return nil
}

// deleteModel deletes a specified model by name.
func deleteModel(modelName string) error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	ctx := context.Background()
	req := &api.DeleteRequest{
		Model: modelName,
	}

	if err := client.Delete(ctx, req); err != nil {
		return fmt.Errorf("failed to delete model: %w", err)
	}

	fmt.Printf("Model '%s' deleted successfully.\n", modelName)
	return nil
}

// getModel retrieves detailed information about a specified model.
func getModel(modelName string) error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	ctx := context.Background()
	req := &api.ShowRequest{
		Model: modelName,
	}

	modelInfo, err := client.Show(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to get model info: %w", err)
	}

	fmt.Printf("Model: %s\nDescription: %s\nLicense: %s\n", modelInfo.ModelInfo, modelInfo.Details, modelInfo.License)
	return nil
}

// getAllModels logs all running models.
func getAllModels() error {
	client, err := api.ClientFromEnvironment()
	if err != nil {
		return fmt.Errorf("failed to create Ollama API client: %w", err)
	}

	ctx := context.Background()
	runningModels, err := client.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("failed to list running models: %w", err)
	}

	fmt.Println("Running Models:")
	for _, model := range runningModels.Models {
		fmt.Printf("- %s\n", model.Model)
	}
	return nil
}
