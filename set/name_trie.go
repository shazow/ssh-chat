package set

import (
    "fmt"
    "sort"
)

type NameTrie struct {
    children   map[rune]*NameTrie
    terminates bool
}

func createNameTrie() *NameTrie {
    return &NameTrie{make(map[rune]*NameTrie), false}
}

func (tree *NameTrie) Exists(name string) bool {
    nameSlice := []rune(name)
    node, ok := tree.traverse(nameSlice, false)
    return ok && node.terminates
}

func (tree *NameTrie) Insert(name string) {
    nameSlice := []rune(name)
    node, ok := tree.traverse(nameSlice, true)
    if ok {
        node.terminates = true
    }
}

func (tree *NameTrie) Delete(name string) {
    nameSlice := []rune(name)
    node, ok := tree.traverse(nameSlice, false)
    if ok {
        node.terminates = false
    }
}

func (tree *NameTrie) ClosestName(prefix string) (name string, ok bool) {
    fmt.Println(tree, prefix)
    nameslice := []rune(prefix)
    node, ok := tree.traverse(nameslice, false)
    if !ok {
        return "", false
    }
    return node.closestName(prefix)
}

func (node *NameTrie) closestName(prefix string) (name string, ok bool) {
    if node.terminates {
        return prefix, true
    }
    keys := []string{}
    for suffix := range(node.children){keys = append(keys, string(suffix))}
    sort.Strings(keys)
    fmt.Println(keys)
    for i := range keys {
        child := node.children[[]rune(keys[i])[0]]
        name, ok := child.closestName(prefix + keys[i])
        if ok{
            return name, true
        }
    }
    return "", false    
}

func (tree *NameTrie) traverse(remainder []rune, create bool) (nextTree *NameTrie, ok bool) {
    nextRune := remainder[0]
    nextTree, ok = tree.children[nextRune]
    if !ok {
        if create {
            tree.children[nextRune] = createNameTrie()
            nextTree = tree.children[nextRune]
        } else {
            return createNameTrie(), false
        }
    }
    if len(remainder) < 2 {
        return nextTree, true
    } else {
        return nextTree.traverse(remainder[1:], create)
    }
}