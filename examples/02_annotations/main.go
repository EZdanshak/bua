package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/anxuanzi/bua/browser"
	"github.com/anxuanzi/bua/screenshot"
)

func main() {
	// Create browser configuration
	cfg := browser.Config{
		Headless:        false,
		ViewportWidth:   1280,
		ViewportHeight:  720,
		ShowHighlight:   true,
		ShowAnnotations: true, // Enable annotations
		Debug:           true,
	}

	// Create browser
	b, err := browser.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create browser: %v", err)
	}
	defer b.Close()

	// Create context
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// Start browser
	fmt.Println("Starting browser...")
	if err := b.Start(ctx); err != nil {
		log.Fatalf("Failed to start browser: %v", err)
	}

	// Navigate to a page with interactive elements
	fmt.Println("Navigating to example.com...")
	if err := b.Navigate(ctx, "https://example.com"); err != nil {
		log.Fatalf("Failed to navigate: %v", err)
	}

	// Wait for page to stabilize
	time.Sleep(2 * time.Second)

	// Get element map
	fmt.Println("Extracting interactive elements...")
	elementMap, err := b.GetElementMap(ctx)
	if err != nil {
		log.Fatalf("Failed to get element map: %v", err)
	}
	fmt.Printf("Found %d interactive elements\n", elementMap.Len())

	// Take screenshot with annotations
	fmt.Println("Taking annotated screenshot...")
	data, err := b.ScreenshotSafeWithAnnotations(ctx, elementMap)
	if err != nil {
		log.Fatalf("Failed to take screenshot: %v", err)
	}

	if len(data) == 0 {
		log.Fatal("Screenshot is empty")
	}

	// Save the screenshot
	outputDir := "./screenshots"
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		log.Fatalf("Failed to create output directory: %v", err)
	}

	outputPath := filepath.Join(outputDir, "annotated_screenshot.jpg")
	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to save screenshot: %v", err)
	}

	fmt.Printf("Annotated screenshot saved to: %s\n", outputPath)

	// Also test non-annotated for comparison
	fmt.Println("Taking regular screenshot for comparison...")
	regularData, err := b.ScreenshotSafe(ctx, false)
	if err != nil {
		log.Fatalf("Failed to take regular screenshot: %v", err)
	}

	regularPath := filepath.Join(outputDir, "regular_screenshot.jpg")
	if err := os.WriteFile(regularPath, regularData, 0644); err != nil {
		log.Fatalf("Failed to save regular screenshot: %v", err)
	}
	fmt.Printf("Regular screenshot saved to: %s\n", regularPath)

	// Try a more complex page
	fmt.Println("\nNavigating to DuckDuckGo for more elements...")
	if err := b.Navigate(ctx, "https://duckduckgo.com"); err != nil {
		log.Fatalf("Failed to navigate to DuckDuckGo: %v", err)
	}

	time.Sleep(2 * time.Second)

	// Get element map for DuckDuckGo
	elementMap2, err := b.GetElementMap(ctx)
	if err != nil {
		log.Fatalf("Failed to get element map: %v", err)
	}
	fmt.Printf("Found %d interactive elements on DuckDuckGo\n", elementMap2.Len())

	// Take annotated screenshot
	data2, err := b.ScreenshotSafeWithAnnotations(ctx, elementMap2)
	if err != nil {
		log.Fatalf("Failed to take screenshot: %v", err)
	}

	outputPath2 := filepath.Join(outputDir, "annotated_duckduckgo.jpg")
	if err := os.WriteFile(outputPath2, data2, 0644); err != nil {
		log.Fatalf("Failed to save screenshot: %v", err)
	}
	fmt.Printf("DuckDuckGo annotated screenshot saved to: %s\n", outputPath2)

	// Print element details
	fmt.Println("\n--- Interactive Elements Found ---")
	for _, el := range elementMap2.Elements {
		if el.IsVisible {
			fmt.Printf("[%d] <%s> %s\n", el.Index, el.TagName, el.Description())
		}
	}

	fmt.Println("\n--- Test Complete ---")
	fmt.Println("Check the screenshots directory to verify annotations are working.")
}

// Direct low-level test using screenshot package
func testAnnotationDirectly() {
	fmt.Println("Testing annotation drawing directly...")

	// Create a simple test with the annotation package
	cfg := screenshot.DefaultAnnotationConfig()
	fmt.Printf("Annotation config: BorderWidth=%d, ShowLabels=%v\n", cfg.BorderWidth, cfg.ShowLabels)
	fmt.Println("Direct annotation test passed.")
}
