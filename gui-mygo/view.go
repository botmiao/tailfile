package main

import (
	"github.com/egoist/mygo/ui"

	"tailfile.local/agent"
)

func (a *app) view(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyC) {
		a.copySelected()
	}
	if c.Shortcut(ui.Cmd, ui.KeyV) {
		a.paste()
	}
	if c.Shortcut(ui.Cmd, ui.KeyR) {
		a.refresh()
	}
	a.followTree()
	if e, ok := a.selectedEntry(); ok {
		a.loadPreview(e)
	}

	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		a.toolbar(c)
		ui.Split(c, &a.sideW, func() { a.treePane(c) }, func() { a.rightPane(c) }).Grow(1)
		a.transferBar(c)
	})
}

func (a *app) toolbar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Padding(10, 12).Gap(8).Background(t.Surface).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Tailfile").Bold()
			ui.Text(c, "MyGo").FontSize(12).TextColor(t.TextMuted)
			ui.TextInput(c, &a.addr).Placeholder("http://127.0.0.1:8645").Width(280)
			if ui.Button(c, "Refresh").Clicked() {
				a.client = agent.NewClient(a.addr)
				a.refresh()
			}
			ui.Text(c, a.status).FontSize(12).TextColor(t.TextMuted).Shrink(1)
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			if a.mode == "push" {
				ui.PrimaryButton(c, "Push").Clicked()
			} else if ui.Button(c, "Push").Clicked() {
				a.mode = "push"
			}
			if a.mode == "pull" {
				ui.PrimaryButton(c, "Pull").Clicked()
			} else if ui.Button(c, "Pull").Clicked() {
				a.mode = "pull"
			}
			ui.Text(c, "A→B").FontSize(12).TextColor(t.TextMuted)
			if ui.Button(c, "Copy").Clicked() {
				a.copySelected()
			}
			if ui.Button(c, "Paste").Clicked() {
				a.paste()
			}
			ui.TextInput(c, &a.newName).Placeholder("New folder").Width(140)
			if ui.Button(c, "Create").Clicked() {
				a.mkdir()
			}
			ui.TextInput(c, &a.renameTo).Placeholder("Rename to").Width(140)
			if ui.Button(c, "Rename").Clicked() {
				a.rename()
			}
			if ui.Button(c, "Cache for drag").Clicked() {
				a.cacheSelected()
			}
			if ui.Button(c, "Reveal").Clicked() {
				a.revealDrag()
			}
		})
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.TextInput(c, &a.importPath).Placeholder("Local path to import").Width(280)
			if ui.Button(c, "Import").Clicked() {
				a.importTyped()
			}
			ui.Text(c, "or drop files onto the list").FontSize(12).TextColor(t.TextMuted)
		})
		if a.err != "" {
			ui.Text(c, a.err).FontSize(12).TextColor(t.Danger).Wrap()
		}
		if a.dragNote != "" {
			row := ui.Row(c).Gap(8).AlignItems(ui.Center)
			if data, ok := dragData(a.dragPath); ok {
				row.DragData(data)
			}
			row.Children(func() {
				ui.Text(c, a.dragNote).FontSize(12)
				if a.dragPath != "" {
					ui.Text(c, "Drag this row out").FontSize(12).TextColor(t.Accent)
				}
			})
		}
	})
}

func (a *app) treePane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Surface).Padding(4).Children(func() {
		ui.Text(c, "Nodes").FontSize(11).TextColor(t.TextMuted).Padding(6, 8)
		if len(a.nodes) == 0 {
			ui.Text(c, "No nodes yet").Padding(8).TextColor(t.TextMuted).Wrap()
			return
		}
		a.tree.List.Label = func(i int) string {
			if i < 0 || i >= a.tree.Rows() {
				return ""
			}
			return a.label(a.tree.Item(i))
		}
		ui.Outline(c, &a.tree, a.rootKeys(), a.children, func(key string) {
			ui.Text(c, a.label(key)).SingleLine()
		}).Grow(1)
	})
}

func (a *app) rootKeys() []string {
	keys := make([]string, 0, len(a.nodes))
	for _, n := range a.nodes {
		keys = append(keys, nodeKey(n.ID))
	}
	return keys
}

func (a *app) children(key string) []string {
	kind, node, path := splitKey(key)
	switch kind {
	case "n":
		for _, n := range a.nodes {
			if n.ID != node {
				continue
			}
			out := make([]string, 0, len(n.ReadRoots))
			for _, p := range n.ReadRoots {
				out = append(out, dirKey(n.ID, p))
			}
			return out
		}
		return []string{}
	case "d":
		if a.tree.Open.Has(key) {
			a.ensureDirs(node, path)
		}
		if kids, ok := a.subdirs[key]; ok {
			return kids
		}
		return []string{}
	default:
		return nil
	}
}

func (a *app) label(key string) string {
	kind, node, path := splitKey(key)
	if kind == "n" {
		for _, n := range a.nodes {
			if n.ID != node {
				continue
			}
			label := n.Name
			if label == "" {
				label = n.ID
			}
			if n.ReadOnly {
				label += "  ·  read-only"
			}
			if n.Self {
				label += "  ·  this machine"
			}
			return label
		}
		return node
	}
	base := agent.BaseRemote(path)
	if base == "" {
		return path
	}
	return base
}

func (a *app) rightPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		head := a.folderP
		if head == "" {
			head = "Select a folder"
		}
		ui.Text(c, head).FontSize(12).TextColor(t.TextMuted).Padding(8, 12).SingleLine()
		ui.SplitVertical(c, &a.listH, func() { a.fileList(c) }, func() { a.preview(c) }).Grow(1)
	})
}

func (a *app) fileList(c *ui.Context) {
	t := c.Theme()
	cols := []ui.TableColumn{
		{Title: "Name", ID: "name", MinWidth: 160},
		{Title: "Size", ID: "size", Width: 90},
		{Title: "Modified", ID: "modified", Width: 170},
	}
	zone := ui.Column(c).Fill()
	if files := zone.DroppedFiles(); len(files) > 0 {
		a.importDropped(files)
	}
	zone.Children(func() {
		if a.folderP == "" {
			ui.Text(c, "Choose a node folder in the tree.").Padding(16).TextColor(t.TextMuted)
			return
		}
		if len(a.entries) == 0 {
			ui.Text(c, "This folder is empty. Drop files from the desktop to import them.").Padding(16).TextColor(t.TextMuted).Wrap()
			return
		}
		table := ui.Table(c, &a.list, cols, len(a.entries), func(row, col int) {
			if row < 0 || row >= len(a.entries) {
				return
			}
			e := a.entries[row]
			switch col {
			case 0:
				name := e.Name
				if e.IsDir {
					name += "/"
				}
				ui.Text(c, name).SingleLine()
			case 1:
				ui.Text(c, formatSize(e.Size, e.IsDir)).TextColor(t.TextMuted)
			default:
				ui.Text(c, e.ModTime.Local().Format("2006-01-02 15:04")).TextColor(t.TextMuted)
			}
		}).Grow(1)
		if table.Submitted() {
			a.openSelectedDir()
		}
	})
}

func (a *app) preview(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(12).Gap(8).Background(t.Surface).Children(func() {
		ui.Text(c, "Preview").FontSize(11).TextColor(t.TextMuted)
		switch {
		case a.previewImg != nil:
			ui.Image(c, a.previewImg).MaxHeight(280).Fit(ui.Contain)
		case a.previewKind == "pdf" && a.pdfView != nil:
			ui.WebView(c, a.pdfView).Height(240).Radius(6)
			if a.previewText != "" {
				ui.Text(c, a.previewText).FontSize(12).Wrap()
			}
		case a.previewKind == "pdf":
			msg := "PDF text"
			if a.pdfErr != "" {
				msg = "WebView could not open the PDF (" + a.pdfErr + "). Text extract:"
			}
			ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted).Wrap()
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Text(c, a.previewText).Wrap()
			})
		case a.previewKind == "text" && a.previewText != "":
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Text(c, a.previewText).Wrap()
			})
		case a.previewKind == "image":
			ui.Text(c, "Could not decode this image.").TextColor(t.TextMuted)
		default:
			ui.Text(c, "Select a file to preview text, an image, or a PDF.").TextColor(t.TextMuted).Wrap()
		}
	})
}

func (a *app) transferBar(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Padding(8, 12).Gap(4).Background(t.Surface).Children(func() {
		ui.Text(c, "Transfers").FontSize(11).TextColor(t.TextMuted)
		if len(a.transfers) == 0 && len(a.imports) == 0 {
			ui.Text(c, "Copy on one node, open a folder on another, paste. Bytes go directly between the two agents.").FontSize(12).TextColor(t.TextMuted).Wrap()
			return
		}
		for _, tr := range a.transfers {
			id := tr.ID
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, tr.Mode+"  "+tr.FromNode+" → "+tr.ToNode+"  "+jobLine(tr.Job)).FontSize(12).Shrink(1)
				if tr.Job.Status == "running" && ui.Button(c.Key("cancel-"+id), "Cancel").Clicked() {
					a.cancelTransfer(id)
				}
				if (tr.Job.Status == "error" || tr.Job.Status == "canceled" || tr.Job.Status == "interrupted") && ui.Button(c.Key("resume-"+id), "Resume").Clicked() {
					a.resumeTransfer(id)
				}
			})
		}
		for _, job := range a.imports {
			ui.Text(c, "import  "+jobLine(job)).FontSize(12)
		}
	})
}
