# Security

## Reporting a problem

Please do not open a public issue for a security problem. Use GitHub's private
reporting instead: open the **Security** tab of this repository and choose
**Report a vulnerability**. You will get an answer within a few days.

## What to expect from ytwatch

- ytwatch has no server and sends nothing except requests to YouTube (see
  [Privacy](README.md#privacy)).
- It runs `yt-dlp`, `mpv` and the helper programs you configure. Commands in
  `config.toml` (`[link.*]` handlers, `mpv.args`, `download.args`) run with your
  rights, so only use a config you trust.
- Text from YouTube (titles, descriptions, channel names) and text you type or
  paste into the search line is cleaned of control characters before it is
  shown, so it cannot send escape codes to your terminal. Tests check this.
- `install.sh` downloads over HTTPS, checks the SHA-256 checksum of the release,
  and extracts only the `ytwatch` file. The checksum comes from the same release,
  so it protects against a damaged or partial download, not against someone who
  controls the release itself.

Only the latest release gets fixes.
