package vecty

import (
	"fmt"

	"github.com/gopherjs/gopherjs/js"
)

// Core implements the Context method of the Component interface, and is the
// core/central struct which all Component implementations should embed.
type Core struct {
	prevRender *HTML
	deleted    bool
}

// Context implements the Component interface.
func (c *Core) Context() *Core { return c }

func (c *Core) Unmount() {
	c.prevRender.Unmount()
}

// Component represents a single visual component within an application. To
// define a new component simply implement the Render method and embed the Core
// struct:
//
// 	type MyComponent struct {
// 		vecty.Core
// 		... additional component fields (state or properties) ...
// 	}
//
// 	func (c *MyComponent) Render() *vecty.HTML {
// 		... rendering ...
// 	}
//
type Component interface {
	// Render is responsible for building HTML which represents the component.
	Render() *HTML

	// Context returns the components context, which is used internally by
	// Vecty in order to store the previous component render for diffing.
	Context() *Core

	//Desc() string
}

type Unmounter interface {
	Unmount()
}

// ComponentOrHTML represents one of:
//
//  Component
//  *HTML
//
// If the underlying value is not one of these types, the code handling the
// value is expected to panic.
type ComponentOrHTML interface{}

// Restorer is an optional interface that Component's can implement in order to
// restore state during component reconciliation and also to short-circuit
// the reconciliation of a Component's body.
type Restorer interface {
	// Restore is called when the component should restore itself against a
	// previous instance of a component. The previous component may be nil or
	// of a different type than this Restorer itself, thus a type assertion
	// should be used.
	//
	// If skip = true is returned, restoration of this component's body is
	// skipped. That is, the component is not rerendered. If the component can
	// prove when Restore is called that the HTML rendered by Component.Render
	// would not change, true should be returned.
	Restore(prev Component) (skip bool)
}

// HTML represents some form of HTML: an element with a specific tag, or some
// literal text (a TextNode).
type HTML struct {
	Node *js.Object

	tag, text       string
	styles, dataset map[string]string
	properties      map[string]interface{}
	eventListeners  []*EventListener
	children        []ComponentOrHTML
	//index           int
}

//var index int

//func (e *HTML) Desc() string {
//	if e == nil {
//		return "nil"
//	}
//	return fmt.Sprint(e.tag, " ", e.index)
//}

func (h *HTML) Unmount() {
	for _, c := range h.children {
		if u, ok := c.(Unmounter); ok {
			u.Unmount()
		}
	}
}

func (h *HTML) restoreText(prev *HTML) {
	h.Node = prev.Node

	// Text modifications.
	if h.text != prev.text {
		h.Node.Set("nodeValue", h.text)
	}
}

func (h *HTML) restoreHTML(prev *HTML) {
	h.Node = prev.Node

	// Properties
	for name, value := range h.properties {
		var oldValue interface{}
		switch name {
		case "value":
			oldValue = h.Node.Get("value").String()
		case "checked":
			oldValue = h.Node.Get("checked").Bool()
		default:
			oldValue = prev.properties[name]
		}
		if value != oldValue {
			h.Node.Set(name, value)
		}
	}
	for name := range prev.properties {
		if _, ok := h.properties[name]; !ok {
			h.Node.Set(name, nil)
		}
	}

	// Styles
	style := h.Node.Get("style")
	for name, value := range h.styles {
		oldValue := prev.styles[name]
		if value != oldValue {
			style.Call("setProperty", name, value)
		}
	}
	for name := range prev.styles {
		if _, ok := h.styles[name]; !ok {
			style.Call("removeProperty", name)
		}
	}

	for _, l := range prev.eventListeners {
		h.Node.Call("removeEventListener", l.Name, l.wrapper)
	}
	for _, l := range h.eventListeners {
		h.Node.Call("addEventListener", l.Name, l.wrapper)
	}

	// TODO better list element reuse
	for i, nextChild := range h.children {
		nextChildRender := doRender(nextChild)
		if i >= len(prev.children) {
			if doRestore(nil, nextChild, nil, nextChildRender) {
				continue
			}
			h.Node.Call("appendChild", nextChildRender.Node)
			continue
		}
		prevChild := prev.children[i]
		prevChildRender, ok := prevChild.(*HTML)
		if !ok {
			c := prevChild.(Component).Context()
			prevChildRender = c.prevRender
			c.deleted = true
		}
		if doRestore(prevChild, nextChild, prevChildRender, nextChildRender) {
			continue
		}
		replaceNode(nextChildRender.Node, prevChildRender.Node)
	}
	for i := len(h.children); i < len(prev.children); i++ {
		prevChild := prev.children[i]
		prevChildRender, ok := prevChild.(*HTML)
		if !ok {
			c := prevChild.(Component).Context()
			prevChildRender = c.prevRender
			c.deleted = true
		}
		prevChildRender.deleteChildren()
		removeNode(prevChildRender.Node)

		//if u, ok := prevChild.(Unmounter); ok {
		//	go u.Unmount()
		//}
	}
}

func (h *HTML) deleteChildren() {
	for _, child := range h.children {
		html, ok := child.(*HTML)
		if !ok {
			c := child.(Component).Context()
			html = c.prevRender
			c.deleted = true
		}
		html.deleteChildren()
	}
}

// Restore implements the Restorer interface.
func (h *HTML) Restore(old ComponentOrHTML) {
	for _, l := range h.eventListeners {
		l := l
		l.wrapper = func(jsEvent *js.Object) {
			if l.callPreventDefault {
				jsEvent.Call("preventDefault")
			}
			if l.callStopPropagation {
				jsEvent.Call("stopPropagation")
			}
			l.Listener(&Event{Object: jsEvent, Target: jsEvent.Get("target")})
		}
	}

	if prev, ok := old.(*HTML); ok && prev != nil {
		if h.text != "" && prev.text != "" {
			h.restoreText(prev)
			return
		}
		if h.tag != "" && prev.tag != "" && h.tag == prev.tag {
			h.restoreHTML(prev)
			return
		}
	}

	if h.tag != "" && h.text != "" {
		panic("vecty: only one of HTML.tag or HTML.text may be set")
	}
	if h.tag != "" {
		h.Node = js.Global.Get("document").Call("createElement", h.tag)
	} else {
		h.Node = js.Global.Get("document").Call("createTextNode", h.text)
	}
	for name, value := range h.properties {
		h.Node.Set(name, value)
	}
	dataset := h.Node.Get("dataset")
	for name, value := range h.dataset {
		dataset.Set(name, value)
	}
	style := h.Node.Get("style")
	for name, value := range h.styles {
		style.Call("setProperty", name, value)
	}
	for _, l := range h.eventListeners {
		h.Node.Call("addEventListener", l.Name, l.wrapper)
	}
	for _, nextChild := range h.children {
		nextChildRender, isHTML := nextChild.(*HTML)
		if !isHTML {
			nextChildComp := nextChild.(Component)
			nextChildRender = nextChildComp.Render()
			nextChildComp.Context().prevRender = nextChildRender
		}

		if doRestore(nil, nextChild, nil, nextChildRender) {
			continue
		}
		h.Node.Call("appendChild", nextChildRender.Node)
	}
}

// Tag returns an HTML element with the given tag name. Generally, this
// function is not used directly but rather the elem subpackage (which is type
// safe) is used instead.
func Tag(tag string, m ...MarkupOrComponentOrHTML) *HTML {
	//index++
	h := &HTML{
		tag: tag,
		//index: index,
	}
	for _, m := range m {
		apply(m, h)
	}
	return h
}

// Text returns a TextNode with the given literal text. Because the returned
// HTML represents a TextNode, the text does not have to be escaped (arbitrary
// user input fed into this function will always be safely rendered).
func Text(text string, m ...MarkupOrComponentOrHTML) *HTML {
	h := &HTML{
		text: text,
	}
	for _, m := range m {
		apply(m, h)
	}
	return h
}

// Rerender causes the body of the given component (i.e. the HTML returned by
// the Component's Render method) to be re-rendered and subsequently restored.
func Rerender(c Component) {
	if c.Context().deleted {
		return
	}
	prevRender := c.Context().prevRender
	nextRender := doRender(c)
	var prevComponent Component = nil // TODO
	if doRestore(prevComponent, c, prevRender, nextRender) {
		return
	}
	if prevRender != nil {
		prevRender.Unmount()
		replaceNode(nextRender.Node, prevRender.Node)
	}
}

func doRender(c ComponentOrHTML) *HTML {
	if h, isHTML := c.(*HTML); isHTML {
		return h
	}
	comp := c.(Component)
	r := comp.Render()
	comp.Context().prevRender = r
	return r
}

func doRestore(prev, next ComponentOrHTML, prevRender, nextRender *HTML) (skip bool) {
	if r, ok := next.(Restorer); ok {
		var p Component
		if prev != nil {
			p = prev.(Component)
		}
		if r.Restore(p) {
			return true
		}
	}
	nextRender.Restore(prevRender)
	return false
}

// RenderBody renders the given component as the document body. The given
// Component's Render method must return a "body" element.
func RenderBody(body Component) {
	nextRender := doRender(body)
	if nextRender.tag != "body" {
		panic(fmt.Sprintf("vecty: RenderBody expected Component.Render to return a body tag, found %q", nextRender.tag))
	}
	doRestore(nil, body, nil, nextRender)
	// TODO: doRestore skip == true here probably implies a user code bug
	doc := js.Global.Get("document")
	if doc.Get("readyState").String() == "loading" {
		doc.Call("addEventListener", "DOMContentLoaded", func() { // avoid duplicate body
			doc.Set("body", nextRender.Node)
		})
		return
	}
	doc.Set("body", nextRender.Node)
}

// SetTitle sets the title of the document.
func SetTitle(title string) {
	js.Global.Get("document").Set("title", title)
}

// AddStylesheet adds an external stylesheet to the document.
func AddStylesheet(url string) {
	link := js.Global.Get("document").Call("createElement", "link")
	link.Set("rel", "stylesheet")
	link.Set("href", url)
	js.Global.Get("document").Get("head").Call("appendChild", link)
}
