package screenshot

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
)

// AnnotationConfig configures how annotations are drawn.
type AnnotationConfig struct {
	// BorderWidth is the width of bounding box borders in pixels.
	BorderWidth int

	// FontSize is the approximate height of index labels in pixels.
	FontSize int

	// ShowLabels determines whether to show index labels on elements.
	ShowLabels bool

	// ShowLabelsOnlyForUnlabeled shows labels only for elements without text.
	ShowLabelsOnlyForUnlabeled bool

	// Colors for different element types.
	LinkColor      color.RGBA
	ButtonColor    color.RGBA
	InputColor     color.RGBA
	DefaultColor   color.RGBA
	LabelBgColor   color.RGBA
	LabelTextColor color.RGBA
}

// DefaultAnnotationConfig returns sensible defaults for annotations.
func DefaultAnnotationConfig() AnnotationConfig {
	return AnnotationConfig{
		BorderWidth:                2,
		FontSize:                   12,
		ShowLabels:                 true,
		ShowLabelsOnlyForUnlabeled: false,
		LinkColor:                  color.RGBA{R: 76, G: 175, B: 80, A: 255},   // Green
		ButtonColor:                color.RGBA{R: 33, G: 150, B: 243, A: 255},  // Blue
		InputColor:                 color.RGBA{R: 255, G: 152, B: 0, A: 255},   // Orange
		DefaultColor:               color.RGBA{R: 156, G: 39, B: 176, A: 255},  // Purple
		LabelBgColor:               color.RGBA{R: 0, G: 0, B: 0, A: 200},       // Semi-transparent black
		LabelTextColor:             color.RGBA{R: 255, G: 255, B: 255, A: 255}, // White
	}
}

// Annotate draws bounding boxes and labels on a screenshot image.
// Following browser-use pattern: boxes around interactive elements with index labels.
func Annotate(imgData []byte, elementMap ElementMapInterface, cfg AnnotationConfig) ([]byte, error) {
	if elementMap == nil || elementMap.Len() == 0 {
		return imgData, nil // No elements to annotate
	}

	// Decode the image
	img, format, err := image.Decode(bytes.NewReader(imgData))
	if err != nil {
		return nil, fmt.Errorf("failed to decode image for annotation: %w", err)
	}

	// Convert to RGBA for drawing
	bounds := img.Bounds()
	rgba := image.NewRGBA(bounds)
	draw.Draw(rgba, bounds, img, bounds.Min, draw.Src)

	// Draw annotations for each element
	for _, el := range elementMap.GetElements() {
		bbox := el.GetBoundingBox()
		if !el.GetIsVisible() || bbox.GetIsEmpty() {
			continue
		}

		// Get color based on element type
		boxColor := getElementColorFromInfo(el, cfg)

		// Draw bounding box
		drawBoundingBoxFromInfo(rgba, bbox, boxColor, cfg.BorderWidth)

		// Draw index label
		if cfg.ShowLabels {
			// If ShowLabelsOnlyForUnlabeled, only show labels for elements without text
			if cfg.ShowLabelsOnlyForUnlabeled && el.GetText() != "" {
				continue
			}
			drawIndexLabelFromInfo(rgba, el.GetIndex(), bbox, cfg)
		}
	}

	// Encode back to original format
	var buf bytes.Buffer
	switch format {
	case "png":
		err = png.Encode(&buf, rgba)
	default:
		err = jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: 85})
	}
	if err != nil {
		return nil, fmt.Errorf("failed to encode annotated image: %w", err)
	}

	return buf.Bytes(), nil
}

// getElementColorFromInfo returns the appropriate color for an element type using the interface.
func getElementColorFromInfo(el ElementInfo, cfg AnnotationConfig) color.RGBA {
	switch el.GetTagName() {
	case "a":
		return cfg.LinkColor
	case "button":
		return cfg.ButtonColor
	case "input", "textarea", "select":
		return cfg.InputColor
	default:
		// Check role
		switch el.GetRole() {
		case "button", "menuitem", "tab":
			return cfg.ButtonColor
		case "link":
			return cfg.LinkColor
		case "textbox", "combobox", "searchbox":
			return cfg.InputColor
		}
		return cfg.DefaultColor
	}
}

// drawBoundingBoxFromInfo draws a rectangle border around the bounding box using the interface.
func drawBoundingBoxFromInfo(img *image.RGBA, bbox BoundingBoxInfo, c color.RGBA, borderWidth int) {
	bounds := img.Bounds()
	x0 := int(bbox.GetX())
	y0 := int(bbox.GetY())
	x1 := int(bbox.GetX() + bbox.GetWidth())
	y1 := int(bbox.GetY() + bbox.GetHeight())

	// Clamp to image bounds
	x0 = clamp(x0, bounds.Min.X, bounds.Max.X-1)
	y0 = clamp(y0, bounds.Min.Y, bounds.Max.Y-1)
	x1 = clamp(x1, bounds.Min.X, bounds.Max.X-1)
	y1 = clamp(y1, bounds.Min.Y, bounds.Max.Y-1)

	// Draw top border
	for y := y0; y < y0+borderWidth && y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			img.Set(x, y, c)
		}
	}

	// Draw bottom border
	for y := y1; y > y1-borderWidth && y >= y0; y-- {
		for x := x0; x <= x1; x++ {
			img.Set(x, y, c)
		}
	}

	// Draw left border
	for x := x0; x < x0+borderWidth && x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			img.Set(x, y, c)
		}
	}

	// Draw right border
	for x := x1; x > x1-borderWidth && x >= x0; x-- {
		for y := y0; y <= y1; y++ {
			img.Set(x, y, c)
		}
	}
}

// drawIndexLabelFromInfo draws the element index at the top center of the bounding box using the interface.
// Uses a simple built-in digit renderer for minimal dependencies.
func drawIndexLabelFromInfo(img *image.RGBA, index int, bbox BoundingBoxInfo, cfg AnnotationConfig) {
	label := fmt.Sprintf("%d", index)
	bounds := img.Bounds()

	// Calculate label position (top center of bounding box)
	charWidth := cfg.FontSize * 7 / 12 // Approximate char width
	charHeight := cfg.FontSize
	padding := 2
	labelWidth := len(label)*charWidth + padding*2
	labelHeight := charHeight + padding*2

	// Position at top center of bounding box
	labelX := int(bbox.GetX()+bbox.GetWidth()/2) - labelWidth/2
	labelY := int(bbox.GetY()) - labelHeight - 2 // Just above the box

	// If label would be above image top, put it inside the box
	if labelY < bounds.Min.Y {
		labelY = int(bbox.GetY()) + 2
	}

	// Clamp to image bounds
	if labelX < bounds.Min.X {
		labelX = bounds.Min.X
	}
	if labelX+labelWidth > bounds.Max.X {
		labelX = bounds.Max.X - labelWidth
	}

	// Draw label background
	for y := labelY; y < labelY+labelHeight && y < bounds.Max.Y; y++ {
		for x := labelX; x < labelX+labelWidth && x < bounds.Max.X; x++ {
			if x >= bounds.Min.X && y >= bounds.Min.Y {
				img.Set(x, y, cfg.LabelBgColor)
			}
		}
	}

	// Draw each character
	textX := labelX + padding
	textY := labelY + padding
	for _, char := range label {
		if char >= '0' && char <= '9' {
			drawDigit(img, int(char-'0'), textX, textY, charWidth, charHeight, cfg.LabelTextColor)
		}
		textX += charWidth
	}
}

// drawDigit draws a single digit using a simple 5x7 pixel pattern.
func drawDigit(img *image.RGBA, digit int, x, y, width, height int, c color.RGBA) {
	// 5x7 bitmap patterns for digits 0-9
	patterns := [][]string{
		{"01110", "10001", "10001", "10001", "10001", "10001", "01110"}, // 0
		{"00100", "01100", "00100", "00100", "00100", "00100", "01110"}, // 1
		{"01110", "10001", "00001", "00110", "01000", "10000", "11111"}, // 2
		{"01110", "10001", "00001", "00110", "00001", "10001", "01110"}, // 3
		{"00010", "00110", "01010", "10010", "11111", "00010", "00010"}, // 4
		{"11111", "10000", "11110", "00001", "00001", "10001", "01110"}, // 5
		{"01110", "10000", "10000", "11110", "10001", "10001", "01110"}, // 6
		{"11111", "00001", "00010", "00100", "01000", "01000", "01000"}, // 7
		{"01110", "10001", "10001", "01110", "10001", "10001", "01110"}, // 8
		{"01110", "10001", "10001", "01111", "00001", "00001", "01110"}, // 9
	}

	if digit < 0 || digit > 9 {
		return
	}

	pattern := patterns[digit]
	scaleX := float64(width) / 5.0
	scaleY := float64(height) / 7.0

	bounds := img.Bounds()

	for row, line := range pattern {
		for col, ch := range line {
			if ch == '1' {
				// Scale and draw pixel
				px := x + int(float64(col)*scaleX)
				py := y + int(float64(row)*scaleY)

				// Fill the scaled area
				for dy := 0; dy < int(math.Ceil(scaleY)); dy++ {
					for dx := 0; dx < int(math.Ceil(scaleX)); dx++ {
						drawX := px + dx
						drawY := py + dy
						if drawX >= bounds.Min.X && drawX < bounds.Max.X &&
							drawY >= bounds.Min.Y && drawY < bounds.Max.Y {
							img.Set(drawX, drawY, c)
						}
					}
				}
			}
		}
	}
}

// clamp restricts a value to be within min and max.
func clamp(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

// AnnotateForLLM annotates a screenshot optimized for LLM vision consumption.
// Uses default annotation config with appropriate sizing.
func AnnotateForLLM(imgData []byte, elementMap ElementMapInterface) ([]byte, error) {
	cfg := DefaultAnnotationConfig()
	// For LLM consumption, we show labels for all elements
	cfg.ShowLabelsOnlyForUnlabeled = false
	return Annotate(imgData, elementMap, cfg)
}

// AnnotateBrowserUseStyle annotates following browser-use pattern:
// Only show index labels for elements without text information.
func AnnotateBrowserUseStyle(imgData []byte, elementMap ElementMapInterface) ([]byte, error) {
	cfg := DefaultAnnotationConfig()
	cfg.ShowLabelsOnlyForUnlabeled = true
	return Annotate(imgData, elementMap, cfg)
}
