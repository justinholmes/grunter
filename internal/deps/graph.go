package deps

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// Graph is a directed acyclic graph of Terragrunt unit dependencies.
type Graph struct {
	// nodes is the set of unit paths in the graph.
	nodes map[string]bool
	// edges maps a unit path to the set of unit paths it depends on.
	edges map[string]map[string]bool
}

// BuildGraph parses terragrunt.hcl files for the given unit paths and constructs
// a dependency graph. Only dependencies between the provided units are tracked.
func BuildGraph(unitPaths []string) (*Graph, error) {
	g := &Graph{
		nodes: make(map[string]bool),
		edges: make(map[string]map[string]bool),
	}

	unitSet := make(map[string]bool, len(unitPaths))
	for _, p := range unitPaths {
		unitSet[p] = true
		g.nodes[p] = true
	}

	for _, unitPath := range unitPaths {
		depPaths, err := parseDependencies(unitPath)
		if err != nil {
			return nil, fmt.Errorf("parsing dependencies for %s: %w", unitPath, err)
		}

		for _, dep := range depPaths {
			resolved := resolveDependencyPath(unitPath, dep)
			if unitSet[resolved] {
				if g.edges[unitPath] == nil {
					g.edges[unitPath] = make(map[string]bool)
				}
				g.edges[unitPath][resolved] = true
			}
		}
	}

	return g, nil
}

// ExecutionLayers returns unit paths grouped into layers for parallel execution.
// Units in the same layer have no dependencies on each other, and all their
// dependencies are in earlier layers.
func (g *Graph) ExecutionLayers() [][]string {
	if len(g.nodes) == 0 {
		return nil
	}

	// Kahn's algorithm for topological sort with layers
	inDegree := make(map[string]int)
	for node := range g.nodes {
		inDegree[node] = 0
	}
	for node, deps := range g.edges {
		if !g.nodes[node] {
			continue
		}
		for dep := range deps {
			if g.nodes[dep] {
				inDegree[node]++
			}
		}
	}

	// Start with nodes that have no dependencies
	var currentLayer []string
	for node, degree := range inDegree {
		if degree == 0 {
			currentLayer = append(currentLayer, node)
		}
	}

	var layers [][]string
	processed := make(map[string]bool)

	for len(currentLayer) > 0 {
		layers = append(layers, currentLayer)
		for _, node := range currentLayer {
			processed[node] = true
		}

		var nextLayer []string
		for node := range g.nodes {
			if processed[node] {
				continue
			}
			allDepsResolved := true
			for dep := range g.edges[node] {
				if g.nodes[dep] && !processed[dep] {
					allDepsResolved = false
					break
				}
			}
			if allDepsResolved {
				nextLayer = append(nextLayer, node)
			}
		}
		currentLayer = nextLayer
	}

	return layers
}

// TopologicalSort returns all units in dependency order.
func (g *Graph) TopologicalSort() ([]string, error) {
	layers := g.ExecutionLayers()
	var result []string
	for _, layer := range layers {
		result = append(result, layer...)
	}

	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("dependency cycle detected: processed %d of %d nodes", len(result), len(g.nodes))
	}
	return result, nil
}

// parseDependencies reads a terragrunt.hcl file and extracts dependency config paths.
func parseDependencies(unitPath string) ([]string, error) {
	hclPath := filepath.Join(unitPath, "terragrunt.hcl")
	data, err := os.ReadFile(hclPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	file, diags := hclsyntax.ParseConfig(data, hclPath, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return nil, fmt.Errorf("parsing HCL: %s", diags.Error())
	}

	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, nil
	}

	var depPaths []string

	for _, block := range body.Blocks {
		switch block.Type {
		case "dependency":
			// dependency "name" { config_path = "../other" }
			for _, attr := range block.Body.Attributes {
				if attr.Name == "config_path" {
					val, valDiags := attr.Expr.Value(nil)
					if !valDiags.HasErrors() && val.Type() == cty.String {
						depPaths = append(depPaths, val.AsString())
					}
				}
			}

		case "dependencies":
			// dependencies { paths = ["../a", "../b"] }
			for _, attr := range block.Body.Attributes {
				if attr.Name == "paths" {
					val, valDiags := attr.Expr.Value(nil)
					if !valDiags.HasErrors() && val.CanIterateElements() {
						it := val.ElementIterator()
						for it.Next() {
							_, v := it.Element()
							if v.Type() == cty.String {
								depPaths = append(depPaths, v.AsString())
							}
						}
					}
				}
			}
		}
	}

	return depPaths, nil
}

// resolveDependencyPath resolves a relative dependency path against the unit's directory.
func resolveDependencyPath(unitPath, depPath string) string {
	if filepath.IsAbs(depPath) {
		return filepath.Clean(depPath)
	}
	return filepath.Clean(filepath.Join(unitPath, depPath))
}
