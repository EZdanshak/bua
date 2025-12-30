// Package main demonstrates basic usage of bua-go with action highlighting.
// This is the simplest possible example - just navigate and click.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/joho/godotenv"

	"github.com/anxuanzi/bua-go"
)

func main() {
	// Load .env file
	_ = godotenv.Load(".env")

	apiKey := os.Getenv("GOOGLE_API_KEY")
	if apiKey == "" {
		log.Fatal("GOOGLE_API_KEY environment variable is required")
	}

	// Create agent with minimal config
	// Highlights are enabled by default in non-headless mode
	agent, err := bua.New(bua.Config{
		APIKey:   apiKey,
		Model:    bua.ModelGemini3Flash,
		Headless: false, // Show browser to see highlights
		Debug:    true,
	})
	if err != nil {
		log.Fatalf("Failed to create agent: %v", err)
	}
	defer agent.Close()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// Start browser
	fmt.Println("Starting browser...")
	if err := agent.Start(ctx); err != nil {
		log.Fatalf("Failed to start: %v", err)
	}

	// Navigate to a page
	fmt.Println("Navigating to example.com...")
	if err := agent.Navigate(ctx, "https://example.com"); err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}

	// Run a simple task - watch for orange corner bracket highlights!
	fmt.Println("Running task (watch for highlights)...")
	result, err := agent.Run(ctx, `Click on "More information..." link.`)
	if err != nil {
		log.Fatalf("Task failed: %v", err)
	}

	fmt.Printf("\nResult: success=%v\n", result.Success)
	fmt.Printf("Steps: %d\n", len(result.Steps))
	for i, step := range result.Steps {
		fmt.Printf("\n--- Step %d: %s ---\n", i+1, step.Action)
		if step.Target != "" {
			fmt.Printf("  Target: %s\n", step.Target)
		}
		if step.Thinking != "" {
			fmt.Printf("  Thinking: %s\n", truncate(step.Thinking, 100))
		}
		if step.Evaluation != "" {
			fmt.Printf("  Evaluation: %s\n", truncate(step.Evaluation, 80))
		}
		if step.NextGoal != "" {
			fmt.Printf("  Next Goal: %s\n", step.NextGoal)
		}
		if step.Reasoning != "" {
			fmt.Printf("  Reasoning: %s\n", step.Reasoning)
		}
		if step.Memory != "" {
			fmt.Printf("  Memory: %s\n", truncate(step.Memory, 80))
		}
	}
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
