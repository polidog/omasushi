import QtQuick
import QtQuick.Controls
import Quickshell
import Quickshell.Io
import qs.Ui
import qs.Commons
import "Model.js" as Model

// Bar widget for omasushi: a sushi glyph with the number of pending actions,
// and a panel listing the plan with Apply / Export / Update buttons. Anything
// that installs or pulls runs in a floating terminal, because yay and git
// may ask questions.
Panel {
  id: root
  moduleName: "polidog.omasushi"
  ipcTarget: "polidog.omasushi"

  property var plan: ({ missing: false, error: false, recipes: [], actions: [], extras: [] })
  property bool loading: false
  property int selectedIndex: 0
  property bool cursorActive: false

  readonly property int refreshMs: Math.max(1, setting("refreshMinutes", 10)) * 60000
  readonly property bool hideWhenUpToDate: setting("hideWhenUpToDate", false) === true
  readonly property int pending: plan.actions.length
  readonly property bool healthy: !plan.missing && !plan.error && plan.recipes.length > 0
  readonly property bool upToDate: healthy && pending === 0

  function refresh() {
    if (planProc.running) return
    loading = true
    planProc.running = true
  }

  // Launch an interactive omasushi command in the Omarchy floating terminal
  // and re-plan once it is gone.
  function runInTerminal(sub) {
    termProc.command = ["omarchy-launch-floating-terminal-with-presentation", Model.terminalCommand(sub)]
    termProc.running = true
    close()
  }

  function moveCursor(delta) {
    var n = plan.actions.length
    if (n === 0) return
    selectedIndex = Math.max(0, Math.min(n - 1, selectedIndex + delta))
  }

  implicitWidth: button.implicitWidth
  implicitHeight: button.implicitHeight

  Component.onCompleted: refresh()
  onOpenedChanged: if (opened) { refresh(); selectedIndex = 0; cursorActive = false }

  Timer {
    interval: root.refreshMs
    running: true
    repeat: true
    onTriggered: root.refresh()
  }

  Process {
    id: planProc
    command: ["bash", "-lc", Model.planScript]
    stdout: StdioCollector {
      waitForEnd: true
      onStreamFinished: {
        root.plan = Model.parsePlan(String(text || ""))
        root.loading = false
      }
    }
  }

  Process {
    id: termProc
    onRunningChanged: if (!running) rePlan.restart()
  }

  // The floating terminal is detached (setsid), so termProc exits at once;
  // poll a few times while the user is likely still inside it.
  Timer {
    id: rePlan
    interval: 15000
    repeat: true
    property int ticks: 0
    onTriggered: { root.refresh(); if (++ticks >= 8) { ticks = 0; stop() } }
  }

  WidgetButton {
    id: button
    anchors.fill: parent
    bar: root.bar
    visible: !(root.hideWhenUpToDate && root.upToDate)
    text: root.upToDate ? "🍣" : (root.healthy ? "🍣 " + root.pending : "🍣 !")
    active: root.healthy && root.pending > 0
    useActiveColor: false
    tooltipText: Model.statusLine(root.plan)
    onPressed: function(b) {
      if (b === Qt.RightButton) root.refresh()
      else root.toggle()
    }
  }

  KeyboardPanel {
    id: panel
    anchorItem: button
    owner: root
    bar: root.bar
    open: root.opened
    focusTarget: keyCatcher
    contentWidth: panel.fittedContentWidth(Style.space(440))
    contentHeight: panel.fittedContentHeight(panelColumn.implicitHeight, Style.space(560))

    PanelKeyCatcher {
      id: keyCatcher
      anchors.fill: parent
      onMoveRequested: function(dx, dy) {
        if (!root.cursorActive) { root.cursorActive = true; return }
        if (dy !== 0) root.moveCursor(dy)
      }
      onActivateRequested: if (root.pending > 0) root.runInTerminal("apply")
      onCloseRequested: root.close()
      onTabRequested: function(direction) { root.switchPanel(direction) }
      onTextKey: function(t) {
        if (t === "r") root.refresh()
        else if (t === "a" && root.pending > 0) root.runInTerminal("apply")
        else if (t === "e") root.runInTerminal("export")
        else if (t === "u") root.runInTerminal("update")
      }

      ScrollView {
        id: scrollArea
        anchors.fill: parent
        clip: true
        ScrollBar.horizontal.policy: ScrollBar.AlwaysOff
        ScrollBar.vertical.policy: panelColumn.implicitHeight > height ? ScrollBar.AsNeeded : ScrollBar.AlwaysOff

        Column {
          id: panelColumn
          width: scrollArea.availableWidth
          spacing: Style.space(14)

          PanelHero {
            width: parent.width
            title: "omasushi"
            meta: root.loading ? "CHECKING…" : Model.statusLine(root.plan)
            foreground: root.bar.foreground
            fontFamily: root.bar.fontFamily
            iconComponent: Text {
              text: "🍣"
              color: root.bar.foreground
              font.family: root.bar.fontFamily
              font.pixelSize: Style.font.display
            }
            trailingControl: PanelActionButton {
              iconText: "󰑐"
              tooltipText: "Refresh (r)"
              foreground: root.bar.foreground
              fontFamily: root.bar.fontFamily
              onClicked: root.refresh()
            }
          }

          // ---------- Problems ----------
          Text {
            visible: root.plan.missing || root.plan.error || root.plan.recipes.length === 0
            width: parent.width
            wrapMode: Text.WordWrap
            text: root.plan.missing
              ? "The omasushi binary is not on PATH. Install it with `go install github.com/polidog/omasushi/cmd/omasushi@latest`."
              : (root.plan.error
                ? "`omasushi plan --json` failed. Run it in a terminal to see why."
                : "No recipe in use yet. Run `omasushi use owner/repo` to pick one.")
            color: Qt.darker(root.bar.foreground, 1.4)
            font.family: root.bar.fontFamily
            font.pixelSize: Style.font.body
          }

          // ---------- Recipes ----------
          PanelSectionHeader {
            visible: root.plan.recipes.length > 0
            width: parent.width
            text: "RECIPES"
            foreground: root.bar.foreground
            fontFamily: root.bar.fontFamily
          }

          Repeater {
            model: root.plan.recipes
            delegate: Text {
              required property var modelData
              width: panelColumn.width
              text: (modelData.local ? "󰉋 " : "󰊤 ") + modelData.name + "  " + modelData.dir
              elide: Text.ElideMiddle
              color: Qt.darker(root.bar.foreground, 1.3)
              font.family: root.bar.fontFamily
              font.pixelSize: Style.font.caption
            }
          }

          // ---------- Pending actions ----------
          PanelSectionHeader {
            visible: root.pending > 0
            width: parent.width
            text: "PENDING"
            foreground: root.bar.foreground
            fontFamily: root.bar.fontFamily
          }

          Repeater {
            model: root.plan.actions

            delegate: CursorSurface {
              id: row
              required property var modelData
              required property int index
              width: panelColumn.width
              implicitHeight: rowText.implicitHeight + Style.spacing.lg
              hasCursor: root.cursorActive && root.selectedIndex === index
              foreground: root.bar.foreground
              fill: Style.hoverFillFor(root.bar.foreground, Color.accent)

              Text {
                id: kindIcon
                anchors.left: parent.left
                anchors.leftMargin: Style.space(10)
                anchors.verticalCenter: parent.verticalCenter
                text: Model.kindIcon(row.modelData.kind)
                color: Color.accent
                font.family: root.bar.fontFamily
                font.pixelSize: Style.font.body
              }

              Column {
                id: rowText
                anchors.left: kindIcon.right
                anchors.leftMargin: Style.space(10)
                anchors.right: parent.right
                anchors.rightMargin: Style.space(10)
                anchors.verticalCenter: parent.verticalCenter
                spacing: Style.space(2)

                Text {
                  width: parent.width
                  text: row.modelData.desc
                  elide: Text.ElideMiddle
                  color: root.bar.foreground
                  font.family: root.bar.fontFamily
                  font.pixelSize: Style.font.body
                }
                Text {
                  width: parent.width
                  text: row.modelData.kind + (row.modelData.recipe ? " · " + row.modelData.recipe : "")
                  elide: Text.ElideRight
                  color: Qt.darker(root.bar.foreground, 1.6)
                  font.family: root.bar.fontFamily
                  font.pixelSize: Style.font.caption
                }
              }

              HoverHandler {
                onHoveredChanged: if (hovered) { root.cursorActive = true; root.selectedIndex = row.index }
              }
            }
          }

          // ---------- Extras ----------
          PanelSectionHeader {
            visible: root.plan.extras.length > 0
            width: parent.width
            text: "INSTALLED BUT NOT IN A RECIPE"
            foreground: root.bar.foreground
            fontFamily: root.bar.fontFamily
          }

          Repeater {
            model: root.plan.extras
            delegate: Text {
              required property var modelData
              width: panelColumn.width
              text: "? " + modelData
              elide: Text.ElideMiddle
              color: Qt.darker(root.bar.foreground, 1.5)
              font.family: root.bar.fontFamily
              font.pixelSize: Style.font.caption
            }
          }

          // ---------- Buttons ----------
          PanelSeparator {
            visible: root.healthy
            foreground: root.bar.foreground
          }

          Row {
            visible: root.healthy
            width: parent.width
            spacing: Style.space(8)

            Button {
              text: "Apply"
              iconText: "󰄬"
              bordered: true
              selected: root.pending > 0
              enabled: root.pending > 0
              foreground: root.bar.foreground
              fontFamily: root.bar.fontFamily
              tooltipText: "omasushi apply (a)"
              onClicked: root.runInTerminal("apply")
            }
            Button {
              text: "Export"
              iconText: "󰈔"
              bordered: true
              enabled: root.plan.extras.length > 0
              foreground: root.bar.foreground
              fontFamily: root.bar.fontFamily
              tooltipText: "omasushi export (e)"
              onClicked: root.runInTerminal("export")
            }
            Button {
              text: "Update"
              iconText: "󰚰"
              bordered: true
              foreground: root.bar.foreground
              fontFamily: root.bar.fontFamily
              tooltipText: "omasushi update (u)"
              onClicked: root.runInTerminal("update")
            }
          }

          Text {
            visible: root.healthy
            width: parent.width
            text: "j/k navigate · a apply · e export · u update · r refresh"
            color: Qt.darker(root.bar.foreground, 1.6)
            font.family: root.bar.fontFamily
            font.pixelSize: Style.font.caption
            horizontalAlignment: Text.AlignHCenter
          }

          Item { width: parent.width; height: Style.space(4) }
        }
      }
    }
  }
}
