.pragma library

// Runs `omasushi plan --json` with the PATH a login shell would have, so a
// binary in ~/go/bin or ~/.local/bin is found even when the shell was
// started before it was installed.
var planScript = 'export PATH="$HOME/go/bin:$HOME/.local/bin:$PATH"; '
  + 'if ! command -v omasushi >/dev/null 2>&1; then echo \'{"missing":true}\'; exit 0; fi; '
  + 'omasushi plan --json 2>/dev/null || echo \'{"error":true}\''

// Same PATH fix for the interactive commands run in a floating terminal.
function terminalCommand(sub) {
  return 'export PATH="$HOME/go/bin:$HOME/.local/bin:$PATH"; omasushi ' + sub
}

function parsePlan(text) {
  var out = { missing: false, error: false, recipes: [], actions: [], extras: [] }
  try {
    var j = JSON.parse(text)
    if (j.missing) { out.missing = true; return out }
    if (j.error) { out.error = true; return out }
    out.recipes = Array.isArray(j.recipes) ? j.recipes : []
    out.actions = Array.isArray(j.actions) ? j.actions : []
    out.extras = Array.isArray(j.extras) ? j.extras : []
  } catch (e) {
    out.error = true
  }
  return out
}

// Nerd Font glyphs per action kind, so the list scans at a glance.
function kindIcon(kind) {
  switch (kind) {
    case "aur":
    case "pacman": return "󰂚"          // package
    case "font": return "󰊣"            // format-font
    case "omarchy-add":
    case "omarchy-enable": return "󰍲"  // puzzle
    case "herdr-add": return "󰍲"
    case "herdr-reload": return "󰑐"    // refresh
    case "file-link": return "󰆅"       // link
    case "skill-link":
    case "command-link": return "󱒱"    // robot
    default:
      if (kind.indexOf("default-") === 0) return "󰂓" // cog
      return "󰂚"
  }
}

function statusLine(plan) {
  if (plan.missing) return "OMASUSHI NOT INSTALLED"
  if (plan.error) return "PLAN FAILED"
  if (plan.recipes.length === 0) return "NO RECIPE IN USE"
  var n = plan.actions.length
  if (n === 0) return "UP TO DATE"
  return (n + (n === 1 ? " PENDING ACTION" : " PENDING ACTIONS"))
}
