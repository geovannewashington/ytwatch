# Thumbnails on Hyprland

**Symptom:** the thumbnail opens as its own tiled window.

ueberzugpp draws the `wayland` thumbnail in a window of its own and can only
place it over the terminal when that window floats. Add a window rule for it.

With a Lua config (as Omarchy ships it), in `hyprland.lua`:

```lua
hl.window_rule({
  match = { class = "^(ueberzugpp_.*)$" },
  float = true, no_anim = true, no_shadow = true, no_focus = true,
})
```

With a Lua config, ueberzugpp 2.9.10 and older still open the thumbnail
centered: they move the window with a legacy `hyprctl dispatch`, which Lua
configs reject. Newer builds (`ueberzugpp-git` on the AUR) support the Lua
config.

With a `hyprland.conf`:

```ini
windowrule = float, class:^(ueberzugpp_.*)$
windowrule = noanim, class:^(ueberzugpp_.*)$
windowrule = noshadow, class:^(ueberzugpp_.*)$
windowrule = nofocus, class:^(ueberzugpp_.*)$
```
