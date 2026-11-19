$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[Windows.Forms.Application]::EnableVisualStyles()
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)

$fa = $env:NETMERGE_UI_LANGUAGE -ne 'en'
$popup = $env:NETMERGE_UI_POPUP -eq 'true'
function L($en, $persian) { if ($fa) { $persian } else { $en } }
function C($hex) { [Drawing.ColorTranslator]::FromHtml($hex) }
$bg = C '#101923'
$panel = C '#1C2C3D'
$fg = C '#EEF5FC'
$muted = C '#ACC0D1'
$accent = C '#75BFFF'
$selected = C '#235982'
$font = [Drawing.Font]::new('Segoe UI', 10)
$script:uiError = $null
$script:firstPaint = $true
function Send($command) {
    [Console]::WriteLine((ConvertTo-Json -InputObject ([string]$command) -Compress))
    [Console]::Out.Flush()
}

$form = [Windows.Forms.Form]::new()
$form.Text = 'NetMerge'
$form.Font = $font
$form.ForeColor = $fg
$form.BackColor = $bg
$form.AutoScaleMode = 'Dpi'
$form.StartPosition = 'CenterScreen'
$form.ShowIcon = $false
if ($fa) { $form.RightToLeft = 'Yes' }
if ($popup) {
    $form.FormBorderStyle = 'None'
    $form.StartPosition = 'Manual'
    $form.ShowInTaskbar = $false
    $form.TopMost = $true
    $form.ClientSize = [Drawing.Size]::new(320, 407)
} else {
    $form.ClientSize = [Drawing.Size]::new(868, 610)
    $form.MinimumSize = [Drawing.Size]::new(770, 610)
}
$root = [Windows.Forms.TableLayoutPanel]::new()
$root.Dock = 'Fill'
$root.Padding = [Windows.Forms.Padding]::new(16)
$root.ColumnCount = 1
$root.GrowStyle = 'AddRows'
$form.Controls.Add($root)
function Row($parent, $height) {
    $i = $parent.RowCount
    $parent.RowCount++
    if ($height -gt 0) { [void]$parent.RowStyles.Add([Windows.Forms.RowStyle]::new('Absolute', $height)) }
    else { [void]$parent.RowStyles.Add([Windows.Forms.RowStyle]::new('Percent', 100)) }
    $i
}
function Label($text, $color, $size) {
    $label = [Windows.Forms.Label]::new()
    $label.Dock = 'Fill'
    $label.Text = $text
    $label.ForeColor = $color
    $label.TextAlign = if ($fa) { 'MiddleRight' } else { 'MiddleLeft' }
    $label.Font = [Drawing.Font]::new('Segoe UI', $size)
    $label
}
function Button($text, $action) {
    $b = [Windows.Forms.Button]::new()
    $b.Text = $text
    $b.Dock = 'Fill'
    $b.FlatStyle = 'Flat'
    $b.FlatAppearance.BorderSize = 0
    $b.FlatAppearance.MouseOverBackColor = $selected
    $b.BackColor = $panel
    $b.ForeColor = $fg
    $b.TextAlign = if ($fa) { 'MiddleRight' } else { 'MiddleLeft' }
    $b.Padding = [Windows.Forms.Padding]::new(8,0,8,0)
    if ($action) {
        $b.Tag = $action
        $b.add_Click({ param($button, $eventArgs) Send $button.Tag })
    }
    $b
}
function Columns($count) {
    $t = [Windows.Forms.TableLayoutPanel]::new()
    $t.Dock = 'Fill'
    $t.ColumnCount = $count
    for ($i=0; $i -lt $count; $i++) { [void]$t.ColumnStyles.Add([Windows.Forms.ColumnStyle]::new('Percent', 100 / $count)) }
    if ($fa) { $t.RightToLeft = 'Yes' }
    $t
}

$title = Label ("NetMerge   " + $env:NETMERGE_UI_VERSION) $fg $(if ($popup) { 16 } else { 24 })
$root.Controls.Add($title, 0, (Row $root $(if ($popup) { 38 } else { 57 })))
$status = Label (L 'Connecting…' 'در حال اتصال…') $accent 10
$root.Controls.Add($status, 0, (Row $root 26))
$metrics = Label '' $fg $(if ($popup) { 10 } else { 15 })
$root.Controls.Add($metrics, 0, (Row $root $(if ($popup) { 33 } else { 53 })))

if ($popup) {
    $root.Controls.Add((Button (L 'Open dashboard' 'باز کردن داشبورد') 'dashboard'), 0, (Row $root 36))
    $toggle = Button (L 'Start merging' 'شروع ادغام') 'start'
    $root.Controls.Add($toggle, 0, (Row $root 36))
    $smart = Button (L 'Smart' 'توزیع هوشمند') 'smart'
    $turn = Button (L 'Round robin' 'توزیع نوبتی') 'round-robin'
    $root.Controls.Add($smart, 0, (Row $root 34))
    $root.Controls.Add($turn, 0, (Row $root 34))
    $root.Controls.Add((Label (L 'Single-flow bonding · relay required' 'ادغام یک اتصال · نیازمند سرور واسط') $muted 9), 0, (Row $root 27))
    $root.Controls.Add((Button (L 'فارسی' 'English') $(if ($fa) {'lang-en'} else {'lang-fa'})), 0, (Row $root 34))
    $root.Controls.Add((Button (L 'Exit NetMerge' 'خروج از NetMerge') 'quit'), 0, (Row $root 34))
    if ($env:NETMERGE_UI_VERSION -ne 'ui-check') { $form.add_Deactivate({ $form.Close() }) }
    $form.add_Shown({
        $p = [Windows.Forms.Cursor]::Position
        $area = [Windows.Forms.Screen]::FromPoint($p).WorkingArea
        $form.Location = [Drawing.Point]::new(
            [Math]::Max($area.Left, [Math]::Min($p.X, $area.Right - $form.Width)),
            [Math]::Max($area.Top, [Math]::Min($p.Y - $form.Height, $area.Bottom - $form.Height)))
        $form.Activate()
    })
} else {
    $table = [Windows.Forms.ListView]::new()
    $table.View = 'Details'
    $table.HeaderStyle = 'Nonclickable'
    $table.Dock = 'Fill'
    $table.FullRowSelect = $true
    $table.MultiSelect = $false
    $table.OwnerDraw = $true
    $table.add_DrawColumnHeader({
        param($sender, $e)
        $brush = [Drawing.SolidBrush]::new($bg)
        try { $e.Graphics.FillRectangle($brush, $e.Bounds) }
        finally { $brush.Dispose() }
        $flags = [Windows.Forms.TextFormatFlags]'VerticalCenter, EndEllipsis'
        if ($fa) { $flags = $flags -bor [Windows.Forms.TextFormatFlags]'RightToLeft, Right' }
        [Windows.Forms.TextRenderer]::DrawText($e.Graphics, $e.Header.Text, $font, $e.Bounds, $muted, $flags)
    })
    $table.add_DrawItem({ param($sender, $e) $e.DrawDefault = $true })
    $table.add_DrawSubItem({ param($sender, $e) $e.DrawDefault = $true })
    $table.HideSelection = $false
    $table.BorderStyle = 'None'
    $table.BackColor = $panel
    $table.ForeColor = $fg
    if ($fa) { $table.RightToLeftLayout = $true }
    foreach ($column in @(
        @((L 'Adapter' 'شبکه'), 178), @((L 'Status' 'وضعیت'), 115),
        @((L 'Down Mbps' 'دانلود Mbps'), 105), @((L 'Up Mbps' 'آپلود Mbps'), 105),
        @((L 'Flows' 'اتصال‌ها'), 76), @((L 'Cap Mbps' 'سقف Mbps'), 95)
    )) { [void]$table.Columns.Add([string]$column[0], [int]$column[1]) }
    $root.Controls.Add($table, 0, (Row $root 0))

    $capBar = Columns 3
    $cap = [Windows.Forms.ComboBox]::new()
    $cap.Dock = 'Fill'
    $cap.DropDownStyle = 'DropDownList'
    $cap.Items.AddRange([object[]]@(0,1,2,5,10,20,50,100))
    $cap.SelectedIndex = 0
    $capBar.Controls.Add((Label (L 'Selected adapter cap (0 = off)' 'سقف شبکهٔ انتخاب‌شده (۰ = بدون محدودیت)') $muted 9), 0, 0)
    $capBar.Controls.Add($cap, 1, 0)
    $apply = Button (L 'Apply cap' 'اعمال سقف سرعت') ''
    $apply.add_Click({
        if ($table.SelectedItems.Count -eq 1) {
            $nic = [Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($table.SelectedItems[0].Text))
            Send ('limit:' + $cap.SelectedItem + ':' + $nic)
        }
    })
    $capBar.Controls.Add($apply, 2, 0)
    $root.Controls.Add($capBar, 0, (Row $root 38))

    $modes = Columns 3
    $smart = Button (L 'Smart' 'توزیع هوشمند') 'smart'
    $turn = Button (L 'Round robin' 'توزیع نوبتی') 'round-robin'
    $modes.Controls.Add($smart, 0, 0)
    $modes.Controls.Add($turn, 1, 0)
    $modes.Controls.Add((Label (L 'Single-flow bonding · relay required' 'ادغام یک اتصال · نیازمند سرور واسط') $muted 9), 2, 0)
    $root.Controls.Add($modes, 0, (Row $root 42))

    $settings = Columns 3
    $v6 = Button '' 'ipv6'
    $auto = Button '' 'auto'
    $signIn = Button '' 'signin'
    $settings.Controls.Add($v6, 0, 0)
    $settings.Controls.Add($auto, 1, 0)
    $settings.Controls.Add($signIn, 2, 0)
    $root.Controls.Add($settings, 0, (Row $root 41))

    $actions = Columns 4
    $toggle = Button (L 'Start merging' 'شروع ادغام') 'start'
    $actions.Controls.Add($toggle, 0, 0)
    $actions.Controls.Add((Button (L 'Releases' 'نسخه‌ها') 'releases'), 1, 0)
    $actions.Controls.Add((Button (L 'فارسی' 'English') $(if ($fa) {'lang-en'} else {'lang-fa'})), 2, 0)
    $actions.Controls.Add((Button (L 'Exit NetMerge' 'خروج از NetMerge') 'quit'), 3, 0)
    $root.Controls.Add($actions, 0, (Row $root 43))
    $errorLine = Label '' (C '#FFAD9E') 9
    $errorLine.AutoEllipsis = $true
    $root.Controls.Add($errorLine, 0, (Row $root 25))
}
$form.KeyPreview = $true
$form.add_KeyDown({ param($sender,$e) if ($e.KeyCode -eq 'Escape') { $form.Close() } })

function PaintState($s) {
    $status.Text = if ($s.On) { L 'Merging is active' 'ادغام فعال است' } elseif ($s.Waiting) { L 'Waiting for two links' 'در انتظار دو اتصال' } else { L 'Paused' 'متوقف' }
    $toggle.Text = if ($s.On) { L 'Pause merging' 'توقف ادغام' } else { L 'Start merging' 'شروع ادغام' }
    $toggle.Tag = if ($s.On) { 'pause' } else { 'start' }
    $smart.BackColor = if ($s.RoundRobin) { $panel } else { $selected }
    $turn.BackColor = if ($s.RoundRobin) { $selected } else { $panel }
    $down = 0.0; $up = 0.0; $online = 0
    if (!$popup) {
        $errorLine.Text = [string]$s.Error
        $v6.Text = (L 'IPv6 only on all links' 'IPv6 فقط با همهٔ لینک‌ها') + $(if ($s.V6Auto) {'  ✓'})
        $auto.Text = (L 'Automatic pause' 'توقف خودکار') + $(if ($s.AutoPause) {'  ✓'})
        $signIn.Text = (L 'Start at sign-in' 'اجرا هنگام ورود') + $(if ($s.SignIn) {'  ✓'})
        $name = if ($table.SelectedItems.Count) { $table.SelectedItems[0].Text }
        $table.BeginUpdate()
        $table.Items.Clear()
    }
    foreach ($a in $s.Adapters) {
        $down += $a.LinkIn; $up += $a.LinkOut
        $isOnline = $a.State -eq 'up' -or $a.State6 -eq 'up'
        if ($isOnline) { $online++ }
        if (!$popup) {
            $item = [Windows.Forms.ListViewItem]::new([string]$a.Name)
            $connection = if ($isOnline) { L 'Online' 'متصل' } elseif ($a.State -eq 'probing') { L 'Checking' 'در حال بررسی' } else { L 'Offline' 'بدون اتصال' }
            $item.ForeColor = if ($isOnline) { $fg } else { $muted }
            $item.SubItems.AddRange([string[]]@($connection, ('{0:F1}' -f ($a.LinkIn * .000008)), ('{0:F1}' -f ($a.LinkOut * .000008)), $a.Active, $a.LimitMbps))
            [void]$table.Items.Add($item)
            if ($a.Name -eq $name) { $item.Selected = $true }
        }
    }
    if (!$popup) { $table.EndUpdate() }
    $metrics.Text = '↓ {0:F1}   ↑ {1:F1} Mbps   ·   {2} {3}' -f ($down * .000008), ($up * .000008), $online, (L 'online' 'متصل')
}

$script:nextLine = $reader.ReadLineAsync()
$timer = [Windows.Forms.Timer]::new()
$timer.Interval = 150
$timer.add_Tick({
    if (!$script:nextLine.IsCompleted) { return }
    try {
        $line = $script:nextLine.GetAwaiter().GetResult()
        if ($null -eq $line) { $form.Close(); return }
        PaintState (ConvertFrom-Json -InputObject $line)
        if ($script:firstPaint) {
            $script:firstPaint = $false
            Send 'ready'
        }
        $script:nextLine = $reader.ReadLineAsync()
    } catch {
        $script:uiError = $_
        $form.Close()
    }
})
$timer.Start()
try { [Windows.Forms.Application]::Run($form) }
finally { $timer.Dispose(); $form.Dispose(); $font.Dispose() }
if ($script:uiError) { throw $script:uiError }
