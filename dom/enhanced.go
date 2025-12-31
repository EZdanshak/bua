// Package dom provides DOM extraction and element mapping functionality.
package dom

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// EnhancedElement extends Element with additional CDP-sourced data.
type EnhancedElement struct {
	Element

	// CDP-specific fields
	BackendNodeID int  `json:"backendNodeId"`
	ParentIndex   int  `json:"parentIndex,omitempty"`
	Depth         int  `json:"depth,omitempty"`
	PaintOrder    int  `json:"paintOrder,omitempty"`
	IsNew         bool `json:"isNew,omitempty"` // New since last snapshot
	IsScrollable  bool `json:"isScrollable,omitempty"`
	ScrollWidth   int  `json:"scrollWidth,omitempty"`
	ScrollHeight  int  `json:"scrollHeight,omitempty"`
	ChildCount    int  `json:"childCount,omitempty"`
	IsShadowRoot  bool `json:"isShadowRoot,omitempty"`

	// Accessibility properties from AX tree
	AXFocusable   bool   `json:"axFocusable,omitempty"`
	AXEditable    bool   `json:"axEditable,omitempty"`
	AXExpanded    *bool  `json:"axExpanded,omitempty"`
	AXChecked     *bool  `json:"axChecked,omitempty"`
	AXSelected    *bool  `json:"axSelected,omitempty"`
	AXRequired    bool   `json:"axRequired,omitempty"`
	AXDisabled    bool   `json:"axDisabled,omitempty"`
	AXDescription string `json:"axDescription,omitempty"`

	// Computed properties for filtering
	ComputedCursor string `json:"computedCursor,omitempty"`
	ZIndex         int    `json:"zIndex,omitempty"`
	IsOccluded     bool   `json:"isOccluded,omitempty"`  // Hidden by paint order
	IsContained    bool   `json:"isContained,omitempty"` // 99% inside parent interactive
}

// EnhancedElementMap extends ElementMap with optimization features.
type EnhancedElementMap struct {
	ElementMap

	// Enhanced tracking
	BackendNodeMap  map[int]*EnhancedElement `json:"-"` // backendNodeId -> element
	PreviousNodeIDs map[int]bool             `json:"-"` // Track previous snapshot for new detection

	// Statistics
	TotalElements     int `json:"totalElements"`
	FilteredByPaint   int `json:"filteredByPaint"`
	FilteredByContain int `json:"filteredByContain"`
	FilteredByHidden  int `json:"filteredByHidden"`
}

// NewEnhancedElementMap creates a new enhanced element map.
func NewEnhancedElementMap() *EnhancedElementMap {
	return &EnhancedElementMap{
		ElementMap: ElementMap{
			Elements: make([]*Element, 0),
			indexMap: make(map[int]*Element),
		},
		BackendNodeMap:  make(map[int]*EnhancedElement),
		PreviousNodeIDs: make(map[int]bool),
	}
}

// AddEnhanced adds an enhanced element to the map.
func (m *EnhancedElementMap) AddEnhanced(el *EnhancedElement) {
	m.Elements = append(m.Elements, &el.Element)
	m.indexMap[el.Index] = &el.Element
	if el.BackendNodeID > 0 {
		m.BackendNodeMap[el.BackendNodeID] = el
	}
}

// ByBackendNodeID returns an element by its backend node ID.
func (m *EnhancedElementMap) ByBackendNodeID(nodeID int) (*EnhancedElement, bool) {
	el, ok := m.BackendNodeMap[nodeID]
	return el, ok
}

// interactiveConfig defines thresholds for interactive detection.
type interactiveConfig struct {
	MinClickableSize     int
	IconSizeMin          int
	IconSizeMax          int
	ContainmentThreshold float64
}

var defaultInteractiveConfig = interactiveConfig{
	MinClickableSize:     5,
	IconSizeMin:          10,
	IconSizeMax:          50,
	ContainmentThreshold: 0.99, // 99% contained
}

// interactiveTags are always considered interactive.
var interactiveTags = map[string]bool{
	"a": true, "button": true, "input": true, "select": true,
	"textarea": true, "details": true, "summary": true,
	"option": true, "optgroup": true,
}

// interactiveRoles from ARIA that indicate interactivity.
var interactiveRoles = map[string]bool{
	"button": true, "link": true, "textbox": true, "checkbox": true,
	"radio": true, "combobox": true, "listbox": true, "menuitem": true,
	"menuitemcheckbox": true, "menuitemradio": true, "option": true,
	"tab": true, "switch": true, "slider": true, "spinbutton": true,
	"searchbox": true, "gridcell": true, "treeitem": true,
}

// DOMSnapshotNode represents a node from DOMSnapshot.captureSnapshot.
type DOMSnapshotNode struct {
	NodeIndex       int
	ParentIndex     int
	NodeType        int
	NodeName        string
	NodeValue       string
	BackendNodeID   int
	Attributes      map[string]string
	BoundingBox     *BoundingBox
	IsVisible       bool
	LayoutNodeIndex int
	PaintOrder      int
	ScrollWidth     int
	ScrollHeight    int
	ClientWidth     int
	ClientHeight    int
}

// ExtractEnhancedElementMap extracts elements using CDP APIs for better accuracy.
// This mirrors browser-use Python's approach with parallel CDP calls.
func ExtractEnhancedElementMap(ctx context.Context, page *rod.Page, previousMap *EnhancedElementMap) (*EnhancedElementMap, error) {
	elementMap := NewEnhancedElementMap()

	// Get page info
	info, err := page.Info()
	if err == nil {
		elementMap.PageURL = info.URL
		elementMap.PageTitle = info.Title
	}

	// Track previous nodes for "new" detection
	if previousMap != nil {
		for nodeID := range previousMap.BackendNodeMap {
			elementMap.PreviousNodeIDs[nodeID] = true
		}
	}

	// Parallel CDP data collection (like browser-use Python)
	var wg sync.WaitGroup
	var snapshotData []byte
	var axTreeData []byte
	var snapshotErr, axTreeErr error

	// 1. Get DOM Snapshot with layout info
	wg.Add(1)
	go func() {
		defer wg.Done()
		snapshotData, snapshotErr = page.Call(ctx, "", "DOMSnapshot.captureSnapshot", map[string]any{
			"computedStyles":            []string{"display", "visibility", "opacity", "cursor", "pointer-events", "overflow"},
			"includeDOMRects":           true,
			"includePaintOrder":         true,
			"includeTextColorOpacities": false,
		})
	}()

	// 2. Get Accessibility Tree
	wg.Add(1)
	go func() {
		defer wg.Done()
		axTreeData, axTreeErr = page.Call(ctx, "", "Accessibility.getFullAXTree", nil)
	}()

	wg.Wait()

	// Build AX lookup first
	axLookup := make(map[int]*CDPAXNode)
	if axTreeErr == nil && len(axTreeData) > 0 {
		var axResponse CDPAXTreeResponse
		if err := json.Unmarshal(axTreeData, &axResponse); err == nil {
			for i := range axResponse.Nodes {
				node := &axResponse.Nodes[i]
				if node.BackendDOMNodeID > 0 {
					axLookup[node.BackendDOMNodeID] = node
				}
			}
		}
	}

	// If CDP snapshot failed, fall back to JavaScript extraction
	if snapshotErr != nil {
		return extractEnhancedElementMapJS(ctx, page, axLookup, elementMap)
	}

	// Parse snapshot response
	elements, err := parseSnapshotResponse(snapshotData, axLookup)
	if err != nil {
		return extractEnhancedElementMapJS(ctx, page, axLookup, elementMap)
	}

	// Filter and index elements
	index := 0
	for _, el := range elements {
		// Skip non-interactive or invisible elements
		if !el.IsInteractive || !el.IsVisible {
			elementMap.FilteredByHidden++
			continue
		}

		// Check if occluded by paint order
		if el.IsOccluded {
			elementMap.FilteredByPaint++
			continue
		}

		// Check if contained within parent interactive element
		if el.IsContained {
			elementMap.FilteredByContain++
			continue
		}

		// Assign index and mark as new if not in previous snapshot
		el.Index = index
		if _, wasPrevious := elementMap.PreviousNodeIDs[el.BackendNodeID]; !wasPrevious && previousMap != nil {
			el.IsNew = true
		}

		elementMap.AddEnhanced(el)
		index++
	}

	elementMap.TotalElements = len(elements)
	return elementMap, nil
}

// parseSnapshotResponse parses DOMSnapshot.captureSnapshot response.
func parseSnapshotResponse(data []byte, axLookup map[int]*CDPAXNode) ([]*EnhancedElement, error) {
	var response struct {
		Documents []struct {
			DocumentURL string `json:"documentURL"`
			BaseURL     string `json:"baseURL"`
			Nodes       struct {
				ParentIndex          []int   `json:"parentIndex"`
				NodeType             []int   `json:"nodeType"`
				NodeName             []int   `json:"nodeName"`
				NodeValue            []int   `json:"nodeValue"`
				BackendNodeID        []int   `json:"backendNodeId"`
				Attributes           [][]int `json:"attributes"`
				TextValue            []int   `json:"textValue"`
				InputValue           []int   `json:"inputValue"`
				CurrentSourceURL     []int   `json:"currentSourceURL"`
				IsClickable          []bool  `json:"isClickable"`
				PseudoType           []int   `json:"pseudoType"`
				ContentDocumentIndex []int   `json:"contentDocumentIndex"`
			} `json:"nodes"`
			Layout struct {
				NodeIndex   []int       `json:"nodeIndex"`
				Bounds      [][]float64 `json:"bounds"`
				PaintOrders []int       `json:"paintOrders"`
				OffsetRects [][]float64 `json:"offsetRects"`
				ScrollRects [][]float64 `json:"scrollRects"`
				ClientRects [][]float64 `json:"clientRects"`
				// Styles is a 2D array: styles[layoutIndex][styleIndex] = stringIndex
				// Maps to the computedStyles we requested: display, visibility, opacity, cursor, pointer-events, overflow
				Styles [][]int `json:"styles"`
			} `json:"layout"`
			Strings []string `json:"strings"`
		} `json:"documents"`
	}

	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("failed to parse snapshot: %w", err)
	}

	if len(response.Documents) == 0 {
		return nil, fmt.Errorf("no documents in snapshot")
	}

	doc := response.Documents[0]
	stringsArr := doc.Strings
	nodes := doc.Nodes
	layout := doc.Layout

	// Build layout lookup
	layoutLookup := make(map[int]int) // nodeIndex -> layoutIndex
	for i, nodeIdx := range layout.NodeIndex {
		layoutLookup[nodeIdx] = i
	}

	// Build attribute lookup
	getAttr := func(nodeIdx int, attrName string) string {
		if nodeIdx >= len(nodes.Attributes) {
			return ""
		}
		attrs := nodes.Attributes[nodeIdx]
		for i := 0; i < len(attrs); i += 2 {
			if i+1 < len(attrs) {
				nameIdx := attrs[i]
				valueIdx := attrs[i+1]
				if nameIdx >= 0 && nameIdx < len(stringsArr) && stringsArr[nameIdx] == attrName {
					if valueIdx >= 0 && valueIdx < len(stringsArr) {
						return stringsArr[valueIdx]
					}
				}
			}
		}
		return ""
	}

	getString := func(idx int) string {
		if idx >= 0 && idx < len(stringsArr) {
			return stringsArr[idx]
		}
		return ""
	}

	elements := make([]*EnhancedElement, 0)

	for i := 0; i < len(nodes.NodeType); i++ {
		// Only process element nodes (type 1)
		if nodes.NodeType[i] != 1 {
			continue
		}

		tagName := ""
		if i < len(nodes.NodeName) {
			tagName = strings.ToLower(getString(nodes.NodeName[i]))
		}

		// Skip non-interactive tags early
		if !shouldProcessTag(tagName) {
			continue
		}

		backendNodeID := 0
		if i < len(nodes.BackendNodeID) {
			backendNodeID = nodes.BackendNodeID[i]
		}

		// Get layout info and computed styles
		var bbox *BoundingBox
		paintOrder := 0
		isVisible := false
		cursor := ""
		overflow := ""
		var scrollWidth, scrollHeight, clientWidth, clientHeight int

		if layoutIdx, ok := layoutLookup[i]; ok {
			if layoutIdx < len(layout.Bounds) {
				bounds := layout.Bounds[layoutIdx]
				if len(bounds) >= 4 {
					bbox = &BoundingBox{
						X:      bounds[0],
						Y:      bounds[1],
						Width:  bounds[2],
						Height: bounds[3],
					}
					isVisible = bbox.Width > 0 && bbox.Height > 0
				}
			}
			if layoutIdx < len(layout.PaintOrders) {
				paintOrder = layout.PaintOrders[layoutIdx]
			}
			// Extract computed styles: display(0), visibility(1), opacity(2), cursor(3), pointer-events(4), overflow(5)
			if layoutIdx < len(layout.Styles) {
				styles := layout.Styles[layoutIdx]
				// Check visibility from computed styles
				if len(styles) > 0 {
					display := getString(styles[0])
					if display == "none" {
						isVisible = false
					}
				}
				if len(styles) > 1 {
					visibility := getString(styles[1])
					if visibility == "hidden" {
						isVisible = false
					}
				}
				if len(styles) > 2 {
					opacityStr := getString(styles[2])
					if opacityStr == "0" {
						isVisible = false
					}
				}
				if len(styles) > 3 {
					cursor = getString(styles[3])
				}
				if len(styles) > 5 {
					overflow = getString(styles[5])
				}
			}
			// Get scroll dimensions from scrollRects and clientRects
			if layoutIdx < len(layout.ScrollRects) {
				scrollRect := layout.ScrollRects[layoutIdx]
				if len(scrollRect) >= 4 {
					scrollWidth = int(scrollRect[2])
					scrollHeight = int(scrollRect[3])
				}
			}
			if layoutIdx < len(layout.ClientRects) {
				clientRect := layout.ClientRects[layoutIdx]
				if len(clientRect) >= 4 {
					clientWidth = int(clientRect[2])
					clientHeight = int(clientRect[3])
				}
			}
		}

		// Get attributes
		role := getAttr(i, "role")
		ariaLabel := getAttr(i, "aria-label")
		placeholder := getAttr(i, "placeholder")
		href := getAttr(i, "href")
		inputType := getAttr(i, "type")
		name := getAttr(i, "name")
		id := getAttr(i, "id")

		// Get text value
		text := ""
		if i < len(nodes.TextValue) && nodes.TextValue[i] >= 0 {
			text = getString(nodes.TextValue[i])
		}
		if text == "" && i < len(nodes.InputValue) && nodes.InputValue[i] >= 0 {
			text = getString(nodes.InputValue[i])
		}

		// Determine interactivity (with cursor-based detection)
		isInteractive := isElementInteractiveWithCursor(tagName, role, getAttr(i, "onclick") != "",
			getAttr(i, "tabindex"), cursor, axLookup[backendNodeID])

		// Check if element is scrollable
		scrollable := isElementScrollable(overflow, scrollWidth, scrollHeight, clientWidth, clientHeight)

		// Check disabled state
		isEnabled := getAttr(i, "disabled") == "" && getAttr(i, "aria-disabled") != "true"

		// Create enhanced element
		el := &EnhancedElement{
			Element: Element{
				TagName:       tagName,
				Role:          role,
				Name:          name,
				Text:          truncate(text, 200),
				Type:          inputType,
				Href:          href,
				Placeholder:   placeholder,
				AriaLabel:     ariaLabel,
				IsVisible:     isVisible,
				IsEnabled:     isEnabled,
				IsInteractive: isInteractive,
			},
			BackendNodeID:  backendNodeID,
			PaintOrder:     paintOrder,
			ComputedCursor: cursor,
			IsScrollable:   scrollable,
			ScrollWidth:    scrollWidth,
			ScrollHeight:   scrollHeight,
		}

		// Merge accessibility info
		if axNode, ok := axLookup[backendNodeID]; ok {
			mergeAXInfo(el, axNode)
		}

		// Generate selector
		if id != "" {
			el.Selector = "#" + id
		} else if name != "" {
			el.Selector = fmt.Sprintf("%s[name=%q]", tagName, name)
		}

		if bbox != nil {
			el.BoundingBox = *bbox
		}

		elements = append(elements, el)
	}

	// Sort by paint order (higher = on top)
	sort.Slice(elements, func(i, j int) bool {
		return elements[i].PaintOrder > elements[j].PaintOrder
	})

	// Mark occluded elements (lower paint order covered by higher)
	markOccludedElements(elements)

	// Mark contained elements (small elements inside larger interactive parents)
	markContainedElements(elements)

	return elements, nil
}

// shouldProcessTag returns true if we should process this tag for interactivity.
func shouldProcessTag(tagName string) bool {
	// Always process interactive tags
	if interactiveTags[tagName] {
		return true
	}
	// Process potential containers that might have interactive roles/attributes
	switch tagName {
	case "div", "span", "li", "tr", "td", "img", "svg", "label", "nav", "header", "footer", "article", "section":
		return true
	}
	return false
}

// isElementInteractive determines if an element is interactive using multi-tier detection.
func isElementInteractive(tagName, role string, hasOnclick bool, tabindex string, axNode *CDPAXNode) bool {
	// Tier 1: Explicit interactive tags
	if interactiveTags[tagName] {
		return true
	}

	// Tier 2: Interactive ARIA roles
	if interactiveRoles[role] {
		return true
	}

	// Tier 3: Event handlers and tabindex
	if hasOnclick {
		return true
	}
	if tabindex != "" && tabindex != "-1" {
		return true
	}

	// Tier 4: Accessibility properties
	if axNode != nil {
		for _, prop := range axNode.Properties {
			if prop.Value == nil {
				continue
			}
			switch prop.Name {
			case "focusable", "editable":
				if val, ok := prop.Value.Value.(bool); ok && val {
					return true
				}
			}
		}
	}

	return false
}

// isElementInteractiveWithCursor extends detection with cursor style.
// cursor: pointer indicates the element is clickable.
func isElementInteractiveWithCursor(tagName, role string, hasOnclick bool, tabindex string, cursor string, axNode *CDPAXNode) bool {
	// First check standard interactive detection
	if isElementInteractive(tagName, role, hasOnclick, tabindex, axNode) {
		return true
	}

	// Tier 5: Cursor-based detection (like browser-use Python)
	if cursor == "pointer" {
		return true
	}

	return false
}

// isElementScrollable checks if an element has overflow scrolling enabled.
func isElementScrollable(overflow string, scrollWidth, scrollHeight, clientWidth, clientHeight int) bool {
	// Check if overflow allows scrolling
	isScrollable := overflow == "auto" || overflow == "scroll" || strings.Contains(overflow, "auto") || strings.Contains(overflow, "scroll")

	// Also check if content exceeds visible area
	hasOverflow := scrollWidth > clientWidth || scrollHeight > clientHeight

	return isScrollable && hasOverflow
}

// mergeAXInfo merges accessibility info into the enhanced element.
func mergeAXInfo(el *EnhancedElement, axNode *CDPAXNode) {
	if axNode.Name != nil {
		if name, ok := axNode.Name.Value.(string); ok && el.Name == "" {
			el.Name = name
		}
	}
	if axNode.Role != nil {
		if role, ok := axNode.Role.Value.(string); ok && el.Role == "" {
			el.Role = role
		}
	}
	if axNode.Description != nil {
		if desc, ok := axNode.Description.Value.(string); ok {
			el.AXDescription = desc
		}
	}

	for _, prop := range axNode.Properties {
		if prop.Value == nil {
			continue
		}
		switch prop.Name {
		case "focusable":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXFocusable = val
				el.IsFocusable = val
			}
		case "editable":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXEditable = val
			}
		case "expanded":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXExpanded = &val
			}
		case "checked":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXChecked = &val
			}
		case "selected":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXSelected = &val
			}
		case "required":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXRequired = val
			}
		case "disabled":
			if val, ok := prop.Value.Value.(bool); ok {
				el.AXDisabled = val
				if val {
					el.IsEnabled = false
				}
			}
		}
	}
}

// markOccludedElements marks elements that are visually occluded by others.
func markOccludedElements(elements []*EnhancedElement) {
	// Elements are already sorted by paint order (highest first)
	// For each element, check if it's significantly covered by elements with higher paint order
	for i, el := range elements {
		if el.BoundingBox.Width <= 0 || el.BoundingBox.Height <= 0 {
			continue
		}

		for j := 0; j < i; j++ {
			higher := elements[j]
			if higher.BoundingBox.Width <= 0 || higher.BoundingBox.Height <= 0 {
				continue
			}

			// Check if higher element significantly covers this one
			if isSignificantlyOccluded(&el.BoundingBox, &higher.BoundingBox) {
				el.IsOccluded = true
				break
			}
		}
	}
}

// markContainedElements marks elements that are 99%+ contained within parent interactive elements.
// This helps filter out redundant child elements (like icons inside buttons).
func markContainedElements(elements []*EnhancedElement) {
	// Build parent-child relationships
	for i, el := range elements {
		if el.BoundingBox.Width <= 0 || el.BoundingBox.Height <= 0 {
			continue
		}

		// Check against all elements that could be parents
		for j, potential := range elements {
			if i == j || !potential.IsInteractive {
				continue
			}
			if potential.BoundingBox.Width <= 0 || potential.BoundingBox.Height <= 0 {
				continue
			}

			// Check if el is significantly contained within potential
			if isContainedWithin(&el.BoundingBox, &potential.BoundingBox, defaultInteractiveConfig.ContainmentThreshold) {
				// If el is smaller and fully inside a larger interactive element, mark as contained
				elArea := el.BoundingBox.Width * el.BoundingBox.Height
				parentArea := potential.BoundingBox.Width * potential.BoundingBox.Height
				if elArea < parentArea {
					el.IsContained = true
					break
				}
			}
		}
	}
}

// isContainedWithin checks if inner is at least threshold% contained within outer.
func isContainedWithin(inner, outer *BoundingBox, threshold float64) bool {
	// Calculate intersection
	x1 := max(inner.X, outer.X)
	y1 := max(inner.Y, outer.Y)
	x2 := min(inner.X+inner.Width, outer.X+outer.Width)
	y2 := min(inner.Y+inner.Height, outer.Y+outer.Height)

	if x2 <= x1 || y2 <= y1 {
		return false // No overlap
	}

	intersectionArea := (x2 - x1) * (y2 - y1)
	innerArea := inner.Width * inner.Height

	if innerArea <= 0 {
		return false
	}

	return intersectionArea/innerArea >= threshold
}

// isSignificantlyOccluded checks if target is mostly covered by occluder.
func isSignificantlyOccluded(target, occluder *BoundingBox) bool {
	// Calculate intersection
	x1 := max(target.X, occluder.X)
	y1 := max(target.Y, occluder.Y)
	x2 := min(target.X+target.Width, occluder.X+occluder.Width)
	y2 := min(target.Y+target.Height, occluder.Y+occluder.Height)

	if x2 <= x1 || y2 <= y1 {
		return false // No overlap
	}

	intersectionArea := (x2 - x1) * (y2 - y1)
	targetArea := target.Width * target.Height

	// Consider occluded if 90% or more is covered
	return intersectionArea/targetArea >= 0.9
}

// extractEnhancedElementMapJS falls back to JavaScript-based extraction.
func extractEnhancedElementMapJS(ctx context.Context, page *rod.Page, axLookup map[int]*CDPAXNode, elementMap *EnhancedElementMap) (*EnhancedElementMap, error) {
	// Fall back to original JavaScript extraction
	basicMap, err := ExtractElementMap(ctx, page)
	if err != nil {
		return nil, err
	}

	// Convert to enhanced format
	elementMap.PageURL = basicMap.PageURL
	elementMap.PageTitle = basicMap.PageTitle

	for _, el := range basicMap.Elements {
		enhanced := &EnhancedElement{
			Element: *el,
		}
		enhanced.BackendNodeID = el.BackendNodeID
		elementMap.AddEnhanced(enhanced)
	}

	return elementMap, nil
}

// ToTokenStringEnhanced converts the enhanced element map with optimization markers.
func (m *EnhancedElementMap) ToTokenStringEnhanced(maxElements int) string {
	var sb strings.Builder

	// Count visible elements
	visibleCount := 0
	for _, el := range m.Elements {
		if el.IsVisible {
			visibleCount++
		}
	}

	sb.WriteString(fmt.Sprintf("Page: %s\n", m.PageTitle))
	sb.WriteString(fmt.Sprintf("URL: %s\n", m.PageURL))

	if maxElements > 0 && visibleCount > maxElements {
		sb.WriteString(fmt.Sprintf("Elements (%d of %d shown):\n", maxElements, visibleCount))
	} else {
		sb.WriteString(fmt.Sprintf("Elements (%d):\n", visibleCount))
	}

	count := 0
	for _, baseEl := range m.Elements {
		if !baseEl.IsVisible {
			continue
		}

		if maxElements > 0 && count >= maxElements {
			break
		}
		count++

		// Find enhanced element
		var enhanced *EnhancedElement
		if el, ok := m.BackendNodeMap[baseEl.BackendNodeID]; ok {
			enhanced = el
		}

		// Format: [index] tag role "text" (type=value, href=url)
		// Mark new elements with asterisk like browser-use Python
		prefix := ""
		if enhanced != nil && enhanced.IsNew {
			prefix = "*"
		}
		sb.WriteString(fmt.Sprintf("%s[%d] %s", prefix, baseEl.Index, baseEl.TagName))

		if baseEl.Role != "" && baseEl.Role != baseEl.TagName {
			sb.WriteString(fmt.Sprintf(" role=%s", baseEl.Role))
		}

		if baseEl.Name != "" {
			sb.WriteString(fmt.Sprintf(" name=%q", truncate(baseEl.Name, 50)))
		} else if baseEl.Text != "" {
			sb.WriteString(fmt.Sprintf(" %q", truncate(baseEl.Text, 50)))
		} else if baseEl.AriaLabel != "" {
			sb.WriteString(fmt.Sprintf(" aria=%q", truncate(baseEl.AriaLabel, 50)))
		} else if baseEl.Placeholder != "" {
			sb.WriteString(fmt.Sprintf(" placeholder=%q", truncate(baseEl.Placeholder, 50)))
		}

		if baseEl.Type != "" {
			sb.WriteString(fmt.Sprintf(" type=%s", baseEl.Type))
		}

		if baseEl.Href != "" {
			sb.WriteString(fmt.Sprintf(" href=%q", truncate(baseEl.Href, 80)))
		}

		// Mark scrollable containers
		if enhanced != nil && enhanced.IsScrollable {
			sb.WriteString(" |SCROLL|")
		}

		if !baseEl.IsEnabled {
			sb.WriteString(" [disabled]")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// Ensure proto is used
var _ = proto.DOMSnapshotCaptureSnapshot{}
