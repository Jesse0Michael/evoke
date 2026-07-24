package evoke

import (
	"fmt"
	"strings"
)

// Render formats a Composition as .evoke file syntax.
func Render(c *Composition) string {
	var b strings.Builder

	writeBlock := func(name string, values []string) {
		if len(values) == 0 {
			return
		}
		fmt.Fprintf(&b, "%s\n", name)
		fmt.Fprintf(&b, "    %s\n", strings.Join(values, ", "))
		b.WriteString("\n")
	}

	writeSingular := func(name, value string) {
		if value == "" {
			return
		}
		fmt.Fprintf(&b, "%s\n", name)
		fmt.Fprintf(&b, "    %s\n", value)
		b.WriteString("\n")
	}

	writeSingular("NAME", c.Name)
	writeBlock("CHARACTER", c.Character)
	writeBlock("PERSONALITY", c.Personality.Positive)
	writeBlock("!PERSONALITY", c.Personality.Negative)
	writeBlock("BACKSTORY", c.Backstory)
	writeBlock("APPEARANCE", c.Appearance.Positive)
	writeBlock("!APPEARANCE", c.Appearance.Negative)
	writeBlock("APPAREL", c.Apparel.Positive)
	writeBlock("!APPAREL", c.Apparel.Negative)
	writeBlock("ENVIRONMENT", c.Environment.Positive)
	writeBlock("!ENVIRONMENT", c.Environment.Negative)
	writeSingular("SCENARIO", c.Scenario)
	writeBlock("PROMPT", c.Prompt.Positive)
	writeBlock("!PROMPT", c.Prompt.Negative)

	for _, img := range c.Images {
		prefix := ""
		if img.Disabled {
			prefix = "!"
		}
		fmt.Fprintf(&b, "%sIMAGE %s\n", prefix, img.Argument)
		for k, v := range img.Settings {
			fmt.Fprintf(&b, "    %s = %s\n", k, v)
		}
		for _, l := range img.Loras {
			fmt.Fprintf(&b, "    @%s\n", l)
		}
		if len(img.Text.Positive) > 0 {
			fmt.Fprintf(&b, "    %s\n", strings.Join(img.Text.Positive, ", "))
		}
		if len(img.Text.Negative) > 0 {
			fmt.Fprintf(&b, "!IMAGE %s\n", img.Argument)
			fmt.Fprintf(&b, "    %s\n", strings.Join(img.Text.Negative, ", "))
		}
		b.WriteString("\n")
	}

	for _, l := range c.Loras {
		fmt.Fprintf(&b, "LORA %s\n", l.Argument)
		for k, v := range l.Settings {
			fmt.Fprintf(&b, "    %s = %s\n", k, v)
		}
		b.WriteString("\n")
	}

	for _, d := range c.Detailers {
		prefix := ""
		if d.Disabled {
			prefix = "!"
		}
		fmt.Fprintf(&b, "%sDETAILER %s\n", prefix, d.Argument)
		for k, v := range d.Settings {
			fmt.Fprintf(&b, "    %s = %s\n", k, v)
		}
		for _, l := range d.Loras {
			fmt.Fprintf(&b, "    @%s\n", l)
		}
		if len(d.Text.Positive) > 0 {
			fmt.Fprintf(&b, "    %s\n", strings.Join(d.Text.Positive, ", "))
		}
		if len(d.Text.Negative) > 0 {
			fmt.Fprintf(&b, "!DETAILER %s\n", d.Argument)
			fmt.Fprintf(&b, "    %s\n", strings.Join(d.Text.Negative, ", "))
		}
		b.WriteString("\n")
	}

	return strings.TrimRight(b.String(), "\n") + "\n"
}
