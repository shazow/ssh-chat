package set

import (
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

func (node *NameTrie) OrderedChildren() []rune {
    keys := []string{}
    for suffix := range(node.children){ keys = append(keys, string(suffix)) }
    sort.Strings(keys)
    sorted_runes := []rune{}
    for i := range(keys){ sorted_runes = append(sorted_runes, rune(keys[i][0]))}
    return sorted_runes
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

func (tree *NameTrie) FirstName(name string) (string, bool){
    nameSlice := []rune(name)
    node, ok := tree.traverse(nameSlice, false)
    searchingName, nameFound := "", false
    if ok {
        c := make(chan string)
        go func(){
            node.AllChildren(name, c)
            close(c)
        }()
        for thisName := range c {
            if ((len(thisName) < len(searchingName)) || !nameFound) {
                searchingName = thisName
                nameFound = true
            }
        }
    }
    return searchingName, nameFound
}

func (node *NameTrie) AllChildren(prefix string, c chan string) {
    if node.terminates {
        c <- prefix
    }
    children := node.OrderedChildren()
    for i := range children {
        new_prefix := children[i]
        child := node.children[new_prefix]
        final_prefix := prefix + string(new_prefix)
        child.AllChildren(final_prefix, c)
    }
}
