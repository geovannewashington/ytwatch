-- ytwatch-ui.lua: the controls of mpv, drawn like those of the YouTube player.
--
-- ytwatch starts mpv with --osc=no and this script. It draws, at the bottom of
-- the window: a red progress bar with the buffered part, the "most replayed"
-- graph and a small picture of the video under the mouse (when ytwatch has them
-- for the video), the play, next, volume, subtitles and full screen buttons, and
-- the time. The title shows at the top. Everything fades out when the mouse is
-- still, and stays while the video is paused.
--
-- ytwatch gives it a JSON file (script-opt ytwatch-ui-data) with the title, the
-- length of the video, the heatmap and the description of the pictures. Every
-- field is optional, and the data is treated as untrusted: it is only drawn as
-- numbers and text.
--
-- For the tests of ytwatch there are two more script options: ytwatch-ui-size
-- ("1280x720") gives the size of the window when there is none, and
-- ytwatch-ui-dump is a file where the last drawing is written, as ASS.

local mp = require("mp")
local utils = require("mp.utils")
local options = require("mp.options")

local opts = { data = "", size = "", dump = "" }
options.read_options(opts, "ytwatch-ui")

------------------------------------------------------------------------------
-- data

local data = {}
if opts.data ~= "" then
    local file = io.open(opts.data, "r")
    if file then
        local text = file:read("*a")
        file:close()
        local parsed = utils.parse_json(text)
        if type(parsed) == "table" then
            data = parsed
        end
    end
end

local function number(value, fallback)
    if type(value) == "number" and value == value and value > -1e12 and value < 1e12 then
        return value
    end
    return fallback
end

-- The heatmap: scores from 0 to 1, evenly spaced over the video.
local heatmap = {}
if type(data.heatmap) == "table" then
    for i = 1, math.min(#data.heatmap, 256) do
        heatmap[i] = math.max(0, math.min(1, number(data.heatmap[i], 0)))
    end
end
if #heatmap < 2 then
    heatmap = {}
end

-- The pictures: a grid of frames in raw BGRA files, one file for each sheet.
local board = nil
if type(data.storyboard) == "table" then
    local b = data.storyboard
    local fw, fh = number(b.width, 0), number(b.height, 0)
    local cols, rows = number(b.cols, 0), number(b.rows, 0)
    local interval, count = number(b.interval, 0), number(b.count, 0)
    if fw >= 8 and fh >= 8 and fw <= 640 and fh <= 640 and cols >= 1 and rows >= 1 and count >= 1
        and (interval > 0 or number(data.duration, 0) > 0) and type(b.dir) == "string" and b.dir ~= "" then
        board = { fw = fw, fh = fh, cols = cols, rows = rows, interval = interval, count = count, dir = b.dir }
    end
end

local title = type(data.title) == "string" and data.title ~= "" and data.title or nil
local media_title = nil

------------------------------------------------------------------------------
-- state

local state = {
    pos = 0, duration = 0, paused = false, volume = 100, mute = false, fullscreen = false,
    chapters = {}, ranges = {}, playlist = 1, sub_visible = false, has_subs = false,
    mouse = { x = -1, y = -1, hover = false },
    last_move = -100, dragging = false, drag_sent = 0,
    visibility = 0, target = 0,
    hover_bar = false, speed = 1,
}
local SPEEDS = { 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2 }
local overlay = mp.create_osd_overlay("ass-events")
local THUMB_ID = 42
local thumb_shown = false
local last_thumb = nil

local function now() return mp.get_time() end

local function osd_size()
    local w, h = mp.get_osd_size()
    if (not w or w <= 0) and opts.size ~= "" then
        local sw, sh = opts.size:match("^(%d+)x(%d+)$")
        if sw then
            return tonumber(sw), tonumber(sh)
        end
    end
    return w or 0, h or 0
end

------------------------------------------------------------------------------
-- ASS helpers

local function clamp(v, lo, hi) return math.max(lo, math.min(hi, v)) end

local function f(v) return string.format("%.1f", v) end

-- Fading: an alpha of 0 is opaque and 255 is clear. `a` is the opacity (0 to 1)
-- that the part has when the controls are fully shown.
local function alpha(a)
    local shown = clamp(a * state.visibility, 0, 1)
    return string.format("&H%02X&", math.floor(255 - 255 * shown + 0.5))
end

local RED, WHITE, BLACK = "0000FF", "FFFFFF", "000000" -- BGR

local function shape(color, a, path, extra)
    return string.format("{\\an7\\pos(0,0)\\bord0\\shad0\\1c&H%s&\\1a%s%s\\p1}%s{\\p0}",
        color, alpha(a), extra or "", path)
end

local function rect(x1, y1, x2, y2, color, a, extra)
    return shape(color, a, string.format("m %s %s l %s %s %s %s %s %s", f(x1), f(y1), f(x2), f(y1), f(x2), f(y2), f(x1), f(y2)), extra)
end

local function rounded(x1, y1, x2, y2, r, color, a, extra)
    r = math.min(r, (x2 - x1) / 2, (y2 - y1) / 2)
    local k = r * 0.5523
    local p = string.format("m %s %s l %s %s b %s %s %s %s %s %s l %s %s b %s %s %s %s %s %s l %s %s b %s %s %s %s %s %s l %s %s b %s %s %s %s %s %s",
        f(x1 + r), f(y1), f(x2 - r), f(y1),
        f(x2 - r + k), f(y1), f(x2), f(y1 + r - k), f(x2), f(y1 + r),
        f(x2), f(y2 - r),
        f(x2), f(y2 - r + k), f(x2 - r + k), f(y2), f(x2 - r), f(y2),
        f(x1 + r), f(y2),
        f(x1 + r - k), f(y2), f(x1), f(y2 - r + k), f(x1), f(y2 - r),
        f(x1), f(y1 + r),
        f(x1), f(y1 + r - k), f(x1 + r - k), f(y1), f(x1 + r), f(y1))
    return shape(color, a, p, extra)
end

local function circle(cx, cy, r, color, a, extra)
    local k = r * 0.5523
    local p = string.format("m %s %s b %s %s %s %s %s %s b %s %s %s %s %s %s b %s %s %s %s %s %s b %s %s %s %s %s %s",
        f(cx - r), f(cy),
        f(cx - r), f(cy - k), f(cx - k), f(cy - r), f(cx), f(cy - r),
        f(cx + k), f(cy - r), f(cx + r), f(cy - k), f(cx + r), f(cy),
        f(cx + r), f(cy + k), f(cx + k), f(cy + r), f(cx), f(cy + r),
        f(cx - k), f(cy + r), f(cx - r), f(cy + k), f(cx - r), f(cy))
    return shape(color, a, p, extra)
end

local function polygon(points, color, a, extra)
    local parts = { string.format("m %s %s", f(points[1][1]), f(points[1][2])) }
    for i = 2, #points do
        parts[#parts + 1] = string.format("l %s %s", f(points[i][1]), f(points[i][2]))
    end
    return shape(color, a, table.concat(parts, " "), extra)
end

-- Text is cut at the braces and the backslashes that would be read as ASS.
local function safe(text)
    text = tostring(text or ""):gsub("[%c]", " ")
    text = text:gsub("\\", "\226\128\139\\"):gsub("{", "("):gsub("}", ")")
    return text
end

local function text(x, y, str, size, color, a, anchor, extra)
    return string.format("{\\an%d\\pos(%s,%s)\\fs%s\\bord1\\shad0\\1c&H%s&\\3c&H000000&\\1a%s\\3a%s%s}%s",
        anchor or 7, f(x), f(y), f(size), color or WHITE, alpha(a or 1), alpha((a or 1) * 0.6), extra or "", safe(str))
end

local function fmt_time(t)
    t = math.max(0, math.floor((t or 0) + 0.0001))
    local h, m, s = math.floor(t / 3600), math.floor(t % 3600 / 60), t % 60
    if h > 0 then
        return string.format("%d:%02d:%02d", h, m, s)
    end
    return string.format("%d:%02d", m, s)
end

-- The width of a text, near enough for a background behind it.
local function text_width(str, size)
    return #tostring(str) * size * 0.56
end

------------------------------------------------------------------------------
-- layout

local W, H, S = 0, 0, 1

local function layout()
    local l = {}
    l.margin = 12 * S
    l.bar_x0, l.bar_x1 = l.margin, W - l.margin
    l.bar_y = H - 50 * S
    l.bar_h = (state.hover_bar or state.dragging) and 5 * S or 3 * S
    l.row_y = H - 24 * S
    l.icon = 24 * S
    -- The buttons are 44 pixels apart, centre to centre.
    local pitch = 44 * S
    l.play_x = l.margin + 16 * S
    l.next_x = l.play_x + pitch
    l.vol_x = l.play_x + ((state.playlist > 1) and 2 or 1) * pitch
    l.time_x = l.vol_x + 30 * S
    l.fs_x = W - l.margin - 16 * S
    l.cc_x = l.fs_x - pitch
    l.speed_x = (state.has_subs and l.cc_x or l.fs_x) - 46 * S
    l.speed_w = 40 * S
    return l
end

local function in_circle(x, y, cx, cy, r)
    return (x - cx) ^ 2 + (y - cy) ^ 2 <= r * r
end

-- What is under the mouse: "bar", "play", "next", "volume", "cc", "fullscreen",
-- "controls" (the strip of the buttons) or "video".
local function hit(x, y, l)
    if y < 0 or x < 0 then return "video" end
    if y >= l.bar_y - 18 * S and y <= l.bar_y + 12 * S and x >= l.bar_x0 - 6 * S and x <= l.bar_x1 + 6 * S then
        return "bar"
    end
    local r = 18 * S
    if in_circle(x, y, l.play_x, l.row_y, r) then return "play" end
    if state.playlist > 1 and in_circle(x, y, l.next_x, l.row_y, r) then return "next" end
    if in_circle(x, y, l.vol_x, l.row_y, r) then return "volume" end
    if state.has_subs and in_circle(x, y, l.cc_x, l.row_y, r) then return "cc" end
    if math.abs(x - l.speed_x) <= l.speed_w / 2 + 4 * S and math.abs(y - l.row_y) <= 16 * S then return "speed" end
    if in_circle(x, y, l.fs_x, l.row_y, r) then return "fullscreen" end
    if y >= H - 70 * S then return "controls" end
    return "video"
end

local function pos_at(x, l)
    if state.duration <= 0 then return 0 end
    return clamp((x - l.bar_x0) / (l.bar_x1 - l.bar_x0), 0, 1) * state.duration
end

local function x_at(t, l)
    if state.duration <= 0 then return l.bar_x0 end
    return l.bar_x0 + clamp(t / state.duration, 0, 1) * (l.bar_x1 - l.bar_x0)
end

------------------------------------------------------------------------------
-- the thumbnail under the mouse

local function remove_thumb()
    if thumb_shown then
        mp.command_native({ name = "overlay-remove", id = THUMB_ID })
        thumb_shown = false
        last_thumb = nil
    end
end

-- The picture of the video at time t: the frame is found in its sheet by its
-- place, and drawn from the raw file with an offset and a stride.
-- The size the picture is drawn at: it grows with the window, as YouTube's
-- does, up to three times the size of a frame.
local function thumb_size()
    local f = clamp(0.2 * W / board.fw, 1, 3)
    return math.floor(board.fw * f + 0.5), math.floor(board.fh * f + 0.5)
end

-- LuaJIT can do the smooth scaling fast. Without it the nearest pixels are used.
local has_ffi, ffi = pcall(require, "ffi")
local has_bit, bit = pcall(require, "bit")
if not has_ffi or not has_bit then ffi = nil end

local scaled_cache = { key = nil, file = nil }
local scaled_flip = 0

-- A frame drawn bigger: the pixels are repeated (the nearest ones are taken)
-- and written to a file that mpv can draw. Two files take turns, so the one
-- mpv is showing is never written.
-- The frame, read into a C array of bytes, and drawn bigger with a smooth
-- (bilinear) scaling. Weights go from 0 to 256.
local function smooth_frame(input, offset, stride, tw, th)
    local fw, fh = board.fw, board.fh
    local source = ffi.new("uint8_t[?]", fw * fh * 4)
    for y = 0, fh - 1 do
        input:seek("set", offset + y * stride)
        local line = input:read(fw * 4)
        if not line or #line < fw * 4 then return nil end
        ffi.copy(source + y * fw * 4, line, fw * 4)
    end

    local x0, x1, wx = ffi.new("int[?]", tw), ffi.new("int[?]", tw), ffi.new("int[?]", tw)
    for x = 0, tw - 1 do
        local at = clamp((x + 0.5) * fw / tw - 0.5, 0, fw - 1)
        local lo = math.floor(at)
        x0[x], x1[x], wx[x] = lo * 4, math.min(lo + 1, fw - 1) * 4, math.floor((at - lo) * 256 + 0.5)
    end

    local out = ffi.new("uint8_t[?]", tw * th * 4)
    for y = 0, th - 1 do
        local at = clamp((y + 0.5) * fh / th - 0.5, 0, fh - 1)
        local lo = math.floor(at)
        local top, bottom = source + lo * fw * 4, source + math.min(lo + 1, fh - 1) * fw * 4
        local wy = math.floor((at - lo) * 256 + 0.5)
        local row = out + y * tw * 4
        for x = 0, tw - 1 do
            local a, b, w = x0[x], x1[x], wx[x]
            for c = 0, 3 do
                local upper = top[a + c] * (256 - w) + top[b + c] * w
                local lower = bottom[a + c] * (256 - w) + bottom[b + c] * w
                row[x * 4 + c] = bit.rshift(upper * (256 - wy) + lower * wy, 16)
            end
        end
    end

    return ffi.string(out, tw * th * 4)
end

local function scaled_frame(path, offset, stride, tw, th)
    local input = io.open(path, "rb")
    if not input then return nil end

    local pixels
    if ffi then
        pixels = smooth_frame(input, offset, stride, tw, th)
        input:close()
        if not pixels then return nil end
        local name = utils.join_path(board.dir, string.format("hover-%d.bgra", scaled_flip))
        scaled_flip = 1 - scaled_flip
        local output = io.open(name, "wb")
        if not output then return nil end
        output:write(pixels)
        output:close()
        return name
    end

    local columns = {}
    for x = 0, tw - 1 do
        columns[x + 1] = math.floor(x * board.fw / tw) * 4 + 1
    end

    local rows, last_row, last_source = {}, nil, nil
    for y = 0, th - 1 do
        local source = math.floor(y * board.fh / th)
        if source ~= last_source then
            input:seek("set", offset + source * stride)
            local line = input:read(board.fw * 4)
            if not line or #line < board.fw * 4 then
                input:close()
                return nil
            end
            local pixels = {}
            for x = 1, tw do
                local p = columns[x]
                pixels[x] = line:sub(p, p + 3)
            end
            last_row, last_source = table.concat(pixels), source
        end
        rows[y + 1] = last_row
    end
    input:close()

    local name = utils.join_path(board.dir, string.format("hover-%d.bgra", scaled_flip))
    scaled_flip = 1 - scaled_flip
    local output = io.open(name, "wb")
    if not output then return nil end
    output:write(table.concat(rows))
    output:close()
    return name
end

local function thumb_command(t, x, y)
    if not board or state.duration <= 0 then return nil end
    local index
    if board.interval > 0 then
        index = math.floor(t * 1000 / board.interval)
    else
        index = math.floor(t / state.duration * board.count)
    end
    index = clamp(index, 0, board.count - 1)

    local per_sheet = board.cols * board.rows
    local sheet = math.floor(index / per_sheet)
    local k = index % per_sheet
    local row, col = math.floor(k / board.cols), k % board.cols
    local stride = board.cols * board.fw * 4

    local path = utils.join_path(board.dir, string.format("sheet-%d.bgra", sheet))
    if not utils.file_info(path) then return nil end

    local offset = row * board.fh * stride + col * board.fw * 4
    local tw, th = thumb_size()
    if tw == board.fw then
        return {
            name = "overlay-add", id = THUMB_ID, x = math.floor(x), y = math.floor(y), file = path,
            offset = offset, fmt = "bgra", w = board.fw, h = board.fh, stride = stride,
        }
    end

    local key = string.format("%s:%d:%d:%d", path, offset, tw, th)
    if scaled_cache.key ~= key then
        scaled_cache.file = scaled_frame(path, offset, stride, tw, th)
        scaled_cache.key = key
    end
    if not scaled_cache.file then return nil end

    return {
        name = "overlay-add", id = THUMB_ID, x = math.floor(x), y = math.floor(y), file = scaled_cache.file,
        offset = 0, fmt = "bgra", w = tw, h = th, stride = tw * 4,
    }
end

local function show_thumb(command)
    if not command then
        remove_thumb()
        return
    end
    local key = string.format("%s:%d:%d:%d", command.file, command.offset, command.x, command.y)
    if key ~= last_thumb then
        mp.command_native(command)
        last_thumb = key
        if opts.dump ~= "" then
            local file = io.open(opts.dump .. ".thumb", "w")
            if file then
                file:write(utils.format_json(command))
                file:close()
            end
        end
    end
    thumb_shown = true
end

------------------------------------------------------------------------------
-- drawing

local function chapter_at(t)
    local found
    for _, chapter in ipairs(state.chapters) do
        if chapter.time <= t + 0.001 then found = chapter else break end
    end
    return found and found.title or nil
end

local function draw_background(parts, l)
    -- A dark band at the bottom and a lighter one at the top. Each layer reaches
    -- the edge of the window, so the layers add up and there are no seams.
    local layers = 22
    local each = 1 - (1 - 0.74) ^ (1 / layers) -- the layers together are 74% dark
    local height = 170 * S
    for i = 0, layers - 1 do
        parts[#parts + 1] = rect(0, H - height * (1 - i / layers), W, H, BLACK, each)
    end
    local top, light = 100 * S, 14
    local each_top = 1 - (1 - 0.55) ^ (1 / light)
    for i = 0, light - 1 do
        parts[#parts + 1] = rect(0, 0, W, top * (1 - i / light), BLACK, each_top)
    end
    local shown = title or media_title
    if shown and shown ~= "" then
        parts[#parts + 1] = text(l.margin + 6 * S, 18 * S, shown, 20 * S, WHITE, 1, 7)
    end
end

local function draw_heatmap(parts, l, played_x)
    if #heatmap == 0 or not (state.hover_bar or state.dragging) then return nil end

    local n = #heatmap
    local base = l.bar_y - l.bar_h / 2 - 3 * S
    local min_h, max_h = 4 * S, 40 * S
    local width = l.bar_x1 - l.bar_x0

    local points, peak = {}, 1
    for i = 1, n do
        points[i] = { l.bar_x0 + width * (i - 1) / (n - 1), base - (min_h + (max_h - min_h) * heatmap[i]) }
        if heatmap[i] > heatmap[peak] then peak = i end
    end

    -- The top line goes through the points as a smooth curve.
    local function at(i) return points[clamp(i, 1, n)] end
    local path = { string.format("m %s %s l %s %s", f(points[1][1]), f(base), f(points[1][1]), f(points[1][2])) }
    for i = 1, n - 1 do
        local p0, p1, p2, p3 = at(i - 1), at(i), at(i + 1), at(i + 2)
        path[#path + 1] = string.format("b %s %s %s %s %s %s",
            f(p1[1] + (p2[1] - p0[1]) / 6), f(math.min(base, p1[2] + (p2[2] - p0[2]) / 6)),
            f(p2[1] - (p3[1] - p1[1]) / 6), f(math.min(base, p2[2] - (p3[2] - p1[2]) / 6)),
            f(p2[1]), f(p2[2]))
    end
    path[#path + 1] = string.format("l %s %s", f(points[n][1]), f(base))
    local graph = table.concat(path, " ")

    parts[#parts + 1] = shape(WHITE, 0.35, graph)
    -- The part that was played is red.
    parts[#parts + 1] = shape(RED, 0.55, graph, string.format("\\clip(0,0,%s,%s)", f(played_x), f(H)))

    return { x = points[peak][1], y = points[peak][2] }
end

local function pill(parts, x, y, str, size, a)
    local w = text_width(str, size) + 14 * S
    local h = size + 8 * S
    local x1 = clamp(x - w / 2, 4 * S, W - w - 4 * S)
    parts[#parts + 1] = rounded(x1, y, x1 + w, y + h, h / 2, BLACK, 0.72 * (a or 1))
    parts[#parts + 1] = text(x1 + w / 2, y + h / 2, str, size, WHITE, a or 1, 5, "\\bord0")
    return x1, w, h
end

local function draw_bar(parts, l)
    local y1, y2 = l.bar_y - l.bar_h / 2, l.bar_y + l.bar_h / 2
    local played_x = x_at(state.pos, l)

    parts[#parts + 1] = rect(l.bar_x0, y1, l.bar_x1, y2, WHITE, 0.3)
    for _, range in ipairs(state.ranges) do
        local a, b = x_at(range.start, l), x_at(range["end"], l)
        if b > a then parts[#parts + 1] = rect(a, y1, b, y2, WHITE, 0.3) end
    end

    local hover_x = nil
    if state.hover_bar and not state.dragging and state.duration > 0 then
        hover_x = clamp(state.mouse.x, l.bar_x0, l.bar_x1)
        if hover_x > played_x then
            parts[#parts + 1] = rect(played_x, y1, hover_x, y2, WHITE, 0.35)
        end
    end

    parts[#parts + 1] = rect(l.bar_x0, y1, played_x, y2, RED, 1)

    -- A gap in the bar where a chapter starts.
    for _, chapter in ipairs(state.chapters) do
        if chapter.time > 1 and state.duration > 0 then
            local x = x_at(chapter.time, l)
            parts[#parts + 1] = rect(x - S, y1, x + S, y2, BLACK, 0.85)
        end
    end

    if state.duration > 0 and (state.hover_bar or state.dragging or state.target > 0) then
        local r = (state.hover_bar or state.dragging) and 7 * S or 0
        if r > 0 then parts[#parts + 1] = circle(played_x, l.bar_y, r, RED, 1) end
    end

    return played_x, hover_x
end

-- The icons are drawn as shapes: they do not need a font.
local function icon_play(parts, cx, cy, s)
    parts[#parts + 1] = polygon({ { cx - 0.38 * s, cy - 0.5 * s }, { cx + 0.5 * s, cy }, { cx - 0.38 * s, cy + 0.5 * s } }, WHITE, 1)
end

local function icon_pause(parts, cx, cy, s)
    parts[#parts + 1] = rect(cx - 0.4 * s, cy - 0.45 * s, cx - 0.1 * s, cy + 0.45 * s, WHITE, 1)
    parts[#parts + 1] = rect(cx + 0.1 * s, cy - 0.45 * s, cx + 0.4 * s, cy + 0.45 * s, WHITE, 1)
end

local function icon_next(parts, cx, cy, s)
    parts[#parts + 1] = polygon({ { cx - 0.45 * s, cy - 0.42 * s }, { cx + 0.25 * s, cy }, { cx - 0.45 * s, cy + 0.42 * s } }, WHITE, 1)
    parts[#parts + 1] = rect(cx + 0.3 * s, cy - 0.42 * s, cx + 0.45 * s, cy + 0.42 * s, WHITE, 1)
end

local function icon_volume(parts, cx, cy, s, level, muted)
    parts[#parts + 1] = polygon({
        { cx - 0.5 * s, cy - 0.18 * s }, { cx - 0.25 * s, cy - 0.18 * s }, { cx + 0.05 * s, cy - 0.45 * s },
        { cx + 0.05 * s, cy + 0.45 * s }, { cx - 0.25 * s, cy + 0.18 * s }, { cx - 0.5 * s, cy + 0.18 * s },
    }, WHITE, 1)
    if muted or level <= 0 then
        -- a cross
        parts[#parts + 1] = polygon({ { cx + 0.2 * s, cy - 0.22 * s }, { cx + 0.3 * s, cy - 0.32 * s }, { cx + 0.55 * s, cy + 0.22 * s }, { cx + 0.45 * s, cy + 0.32 * s } }, WHITE, 1)
        parts[#parts + 1] = polygon({ { cx + 0.45 * s, cy - 0.32 * s }, { cx + 0.55 * s, cy - 0.22 * s }, { cx + 0.3 * s, cy + 0.32 * s }, { cx + 0.2 * s, cy + 0.22 * s } }, WHITE, 1)
        return
    end
    local waves = level < 40 and 1 or 2
    for i = 1, waves do
        local r = (0.18 + 0.2 * i) * s
        local pts = {}
        for k = -4, 4 do
            local ang = k / 4 * 0.9
            pts[#pts + 1] = { cx + 0.1 * s + r * math.cos(ang), cy + r * math.sin(ang) }
        end
        local back = {}
        for k = 4, -4, -1 do
            local ang = k / 4 * 0.9
            back[#back + 1] = { cx + 0.1 * s + (r - 0.07 * s) * math.cos(ang), cy + (r - 0.07 * s) * math.sin(ang) }
        end
        for _, p in ipairs(back) do pts[#pts + 1] = p end
        parts[#parts + 1] = polygon(pts, WHITE, 1)
    end
end

local function icon_cc(parts, cx, cy, s, on)
    local x1, y1, x2, y2 = cx - 0.55 * s, cy - 0.4 * s, cx + 0.55 * s, cy + 0.4 * s
    parts[#parts + 1] = rounded(x1, y1, x2, y2, 0.14 * s, WHITE, on and 1 or 0.75)
    parts[#parts + 1] = text(cx, cy, "CC", 0.5 * s, BLACK, 1, 5, "\\bord0\\b1")
    if on then
        parts[#parts + 1] = rect(x1, y2 + 0.1 * s, x2, y2 + 0.2 * s, RED, 1)
    end
end

-- The speed: a small button with the speed on it, that goes to the next one.
local function icon_speed(parts, l)
    local label = string.format("%gx", state.speed)
    local x1, x2 = l.speed_x - l.speed_w / 2, l.speed_x + l.speed_w / 2
    local y1, y2 = l.row_y - 11 * S, l.row_y + 11 * S
    local changed = math.abs(state.speed - 1) > 0.001
    parts[#parts + 1] = rounded(x1, y1, x2, y2, 11 * S, WHITE, changed and 0.9 or 0.22)
    parts[#parts + 1] = text(l.speed_x, l.row_y, label, 15 * S, changed and BLACK or WHITE, 1, 5, "\\bord0\\b1")
end

local function icon_fullscreen(parts, cx, cy, s, full)
    -- Four corners. To enter full screen their arms point to the middle, to
    -- leave it they point away from it.
    local reach, gap, len, t = 0.42 * s, 0.16 * s, 0.3 * s, 0.09 * s
    for _, sx in ipairs({ -1, 1 }) do
        for _, sy in ipairs({ -1, 1 }) do
            local px, py, ax, ay
            if full then
                px, py, ax, ay = cx + sx * gap, cy + sy * gap, sx, sy
            else
                px, py, ax, ay = cx + sx * reach, cy + sy * reach, -sx, -sy
            end
            local hx = px + ax * len
            local vy = py + ay * len
            parts[#parts + 1] = rect(math.min(px, hx) - t / 2, py - t / 2, math.max(px, hx) + t / 2, py + t / 2, WHITE, 1)
            parts[#parts + 1] = rect(px - t / 2, math.min(py, vy) - t / 2, px + t / 2, math.max(py, vy) + t / 2, WHITE, 1)
        end
    end
end

local function draw_controls(parts, l)
    local s = l.icon
    if state.paused then icon_play(parts, l.play_x, l.row_y, s) else icon_pause(parts, l.play_x, l.row_y, s) end
    if state.playlist > 1 then icon_next(parts, l.next_x, l.row_y, s) end
    icon_volume(parts, l.vol_x, l.row_y, s, state.volume, state.mute)

    local label
    if state.duration > 0 then
        label = fmt_time(state.pos) .. " / " .. fmt_time(state.duration)
    else
        label = fmt_time(state.pos)
    end
    parts[#parts + 1] = text(l.time_x, l.row_y, label, 15 * S, WHITE, 0.92, 4)

    icon_speed(parts, l)
    if state.has_subs then icon_cc(parts, l.cc_x, l.row_y, s, state.sub_visible) end
    icon_fullscreen(parts, l.fs_x, l.row_y, s, state.fullscreen)
end

local function draw_hover(parts, l, hover_x, peak)
    if not hover_x then
        remove_thumb()
        return
    end

    local t = pos_at(hover_x, l)
    local top = l.bar_y - (#heatmap > 0 and 52 or 14) * S

    local tw, th = 0, 0
    if board then
        tw, th = thumb_size()
    end

    local command = nil
    local ty = top - th - 34 * S
    if board then
        local tx = clamp(hover_x - tw / 2, 6 * S, W - tw - 6 * S)
        command = thumb_command(t, tx, ty)
        if command then
            -- A border around the picture.
            local b = 2
            parts[#parts + 1] = rect(tx - b, ty - b, tx + tw + b, ty, WHITE, 1)
            parts[#parts + 1] = rect(tx - b, ty + th, tx + tw + b, ty + th + b, WHITE, 1)
            parts[#parts + 1] = rect(tx - b, ty, tx, ty + th, WHITE, 1)
            parts[#parts + 1] = rect(tx + tw, ty, tx + tw + b, ty + th, WHITE, 1)
        end
    end
    show_thumb(command)

    -- The chapter and the time, under the picture or alone.
    local label_y = command and (ty + th + 6 * S) or (top - 26 * S)
    local chapter = chapter_at(t)
    if chapter and chapter ~= "" then
        local size = 14 * S
        parts[#parts + 1] = text(clamp(hover_x, tw / 2 + 6 * S, W - tw / 2 - 6 * S), label_y, chapter, size, WHITE, 1, 8)
        label_y = label_y + size + 4 * S
    end
    pill(parts, hover_x, label_y, fmt_time(t), 15 * S, 1)

    -- "Most replayed" near the highest point, if the picture is not in the way.
    if peak and math.abs(hover_x - peak.x) > tw / 2 + 60 * S then
        pill(parts, peak.x, peak.y - 32 * S, "Most replayed", 14 * S, 0.95)
    end
end

local function dump(parts)
    if opts.dump == "" then return end
    local file = io.open(opts.dump, "w")
    if not file then return end
    file:write(string.format("[Script Info]\nScriptType: v4.00+\nPlayResX: %d\nPlayResY: %d\n\n", W, H))
    file:write("[V4+ Styles]\nFormat: Name,Fontname,Fontsize,PrimaryColour,SecondaryColour,OutlineColour,BackColour,Bold,Italic,Underline,StrikeOut,ScaleX,ScaleY,Spacing,Angle,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding\n")
    file:write("Style: Default,sans-serif,20,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,7,0,0,0,1\n\n")
    file:write("[Events]\nFormat: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text\n")
    for _, line in ipairs(parts) do
        file:write("Dialogue: 0,0:00:00.00,9:00:00.00,Default,,0,0,0,," .. line .. "\n")
    end
    file:close()
end

local function render()
    local w, h = osd_size()
    if w <= 0 or h <= 0 then return end
    W, H = w, h
    S = clamp(h / 720, 0.8, 2.5)

    if state.visibility <= 0.01 then
        overlay.data = ""
        overlay:update()
        remove_thumb()
        dump({})
        return
    end

    local l = layout()
    local parts = {}

    draw_background(parts, l)
    local played_x, hover_x = draw_bar(parts, l)
    local peak = draw_heatmap(parts, l, played_x)
    draw_controls(parts, l)
    draw_hover(parts, l, hover_x, peak)

    overlay.res_x, overlay.res_y = W, H
    overlay.data = table.concat(parts, "\n")
    overlay:update()
    dump(parts)
end

------------------------------------------------------------------------------
-- state and events

local function wake()
    state.last_move = now()
end

local function update_target()
    local near_controls = state.mouse.hover and state.mouse.y >= 0 and (state.mouse.y >= H - 130 * S or state.mouse.y <= 90 * S)
    local recent = now() - state.last_move < 2.5
    state.target = (state.paused or state.dragging or recent or near_controls) and 1 or 0
end

local function tick()
    local w, h = osd_size()
    W, H = w, h
    S = clamp(h / 720, 0.8, 2.5)
    update_target()

    -- The fade takes about a fifth of a second.
    local step = 0.2
    local before = state.visibility
    if state.visibility < state.target then
        state.visibility = math.min(state.target, state.visibility + step)
    elseif state.visibility > state.target then
        state.visibility = math.max(state.target, state.visibility - step * 0.7)
    end

    if state.visibility > 0 or before > 0 then
        render()
    end
end

local function update_hover_flags()
    local l = layout()
    local what = hit(state.mouse.x, state.mouse.y, l)
    local was = state.hover_bar
    state.hover_bar = state.mouse.hover and (what == "bar") or false
    if was and not state.hover_bar and not state.dragging then
        remove_thumb()
    end
    return what, l
end

mp.observe_property("mouse-pos", "native", function(_, value)
    if not value then return end
    local moved = value.x ~= state.mouse.x or value.y ~= state.mouse.y
    -- Without a window mpv never says that the mouse is over it: in the tests
    -- (which give a size) it is taken to be.
    state.mouse = { x = value.x, y = value.y, hover = value.hover or opts.size ~= "" }
    if moved then wake() end
    local what = update_hover_flags()
    if opts.dump ~= "" then
        mp.msg.info(string.format("ytwatch-ui mouse %s,%s -> %s (W=%s H=%s S=%s)", value.x, value.y, what, W, H, S))
    end

    if state.dragging and state.duration > 0 and now() - state.drag_sent > 0.06 then
        state.drag_sent = now()
        mp.command(string.format("seek %.3f absolute+keyframes", pos_at(state.mouse.x, layout())))
    end
end)

-- Videos in a queue have no data of their own, and show the title mpv knows.
mp.observe_property("media-title", "string", function(_, v) media_title = type(v) == "string" and v or nil end)
mp.observe_property("time-pos", "number", function(_, v) state.pos = v or 0 end)
mp.observe_property("duration", "number", function(_, v) state.duration = (v and v > 0) and v or number(data.duration, 0) end)
mp.observe_property("pause", "bool", function(_, v) state.paused = v and true or false; wake() end)
mp.observe_property("volume", "number", function(_, v) state.volume = v or 100 end)
mp.observe_property("speed", "number", function(_, v) state.speed = (type(v) == "number" and v > 0) and v or 1 end)
mp.observe_property("mute", "bool", function(_, v) state.mute = v and true or false end)
mp.observe_property("fullscreen", "bool", function(_, v) state.fullscreen = v and true or false end)
mp.observe_property("playlist-count", "number", function(_, v) state.playlist = v or 1 end)
mp.observe_property("sub-visibility", "bool", function(_, v) state.sub_visible = v and true or false end)
mp.observe_property("chapter-list", "native", function(_, v)
    local chapters = {}
    if type(v) == "table" then
        for i = 1, math.min(#v, 500) do
            local c = v[i]
            if type(c) == "table" and number(c.time, nil) then
                chapters[#chapters + 1] = { time = c.time, title = type(c.title) == "string" and c.title or "" }
            end
        end
    end
    state.chapters = chapters
end)
mp.observe_property("track-list", "native", function(_, v)
    local has = false
    if type(v) == "table" then
        for _, track in ipairs(v) do
            if track.type == "sub" and track.lang ~= "live_chat" and track.title ~= "json" then has = true end
        end
    end
    state.has_subs = has
end)
mp.observe_property("demuxer-cache-state", "native", function(_, v)
    local ranges = {}
    if type(v) == "table" and type(v["seekable-ranges"]) == "table" then
        for i = 1, math.min(#v["seekable-ranges"], 16) do
            local r = v["seekable-ranges"][i]
            if number(r.start, nil) and number(r["end"], nil) then ranges[#ranges + 1] = { start = r.start, ["end"] = r["end"] } end
        end
    end
    state.ranges = ranges
end)

-- Clicks: the bar seeks, the buttons do what they show, the video pauses. A
-- click is a "down" and an "up" from a real mouse, and a single "press" when it
-- is sent as one (by the mouse command, for example).
local function press(what, l)
    if what == "bar" and state.duration > 0 then
        state.dragging = true
        state.drag_sent = now()
        mp.command(string.format("seek %.3f absolute+keyframes", pos_at(state.mouse.x, l)))
    elseif what == "play" or what == "video" then
        mp.command("cycle pause")
    elseif what == "next" then
        mp.command("playlist-next")
    elseif what == "volume" then
        mp.command("cycle mute")
    elseif what == "speed" then
        local next_speed = SPEEDS[1]
        for _, v in ipairs(SPEEDS) do
            if v > state.speed + 0.001 then
                next_speed = v
                break
            end
        end
        mp.command(string.format("set speed %g", next_speed))
    elseif what == "cc" then
        mp.command("cycle sub-visibility")
    elseif what == "fullscreen" then
        mp.command("cycle fullscreen")
    end
end

local function release(l)
    if not state.dragging then return end
    state.dragging = false
    if state.duration > 0 then
        mp.command(string.format("seek %.3f absolute+exact", pos_at(state.mouse.x, l)))
    end
    update_hover_flags()
end

mp.add_forced_key_binding("mbtn_left", "ytwatch-ui-click", function(event)
    local what, l = update_hover_flags()
    wake()
    if opts.dump ~= "" then
        mp.msg.info(string.format("ytwatch-ui click %s on %s (dragging=%s)", tostring(event.event), what, tostring(state.dragging)))
    end

    local kind = event.event
    if kind == "down" or kind == "press" then press(what, l) end
    if kind == "up" or kind == "press" then release(l) end
end, { complex = true })

mp.register_event("end-file", function()
    state.dragging = false
    remove_thumb()
end)

mp.register_event("shutdown", function()
    remove_thumb()
end)

-- mpv writes the position of a video only when it quits. A mpv that is killed
-- or crashes would lose it, so it is written every half minute while playing.
-- It does nothing unless ytwatch asked mpv to save positions.
mp.add_periodic_timer(30, function()
    if not mp.get_property_native("save-position-on-quit") then return end
    if state.paused or not mp.get_property_native("time-pos") then return end
    mp.command("write-watch-later-config")
end)

mp.add_periodic_timer(1 / 30, function()
    local ok, err = pcall(tick)
    if not ok then
        mp.msg.error("ytwatch-ui: " .. tostring(err))
    end
end)

wake()
