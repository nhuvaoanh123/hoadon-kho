# Smoke test of the shipped HoaDonKho.exe on a clean Windows machine (used by CI).
# Starts the exe like a user would, imports an invoice through the HTTP API, exports every
# Excel report and checks OCR. Usage: powershell -File scripts/smoke.ps1 -Exe dist/HoaDonKho.exe
param([Parameter(Mandatory = $true)][string]$Exe, [int]$Port = 8799)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$exePath = (Resolve-Path $Exe).Path
$data = Join-Path ([IO.Path]::GetTempPath()) ("hdk-smoke-" + [guid]::NewGuid())
$base = "http://127.0.0.1:$Port"
$hdr = @{ "X-App" = "hoadon-kho" }
function Count($x) { @($x | Where-Object { $null -ne $_ }).Count }
function Check($ok, $what) { if (-not $ok) { throw "FAIL: $what" }; Write-Host "ok   $what" }

Write-Host "OS: $((Get-CimInstance Win32_OperatingSystem).Caption) $([Environment]::OSVersion.Version)"
$proc = Start-Process -FilePath $exePath -ArgumentList "-no-browser", "-port", "$Port", "-data", "`"$data`"" -PassThru -WindowStyle Hidden
try {
  $up = $false
  for ($i = 0; $i -lt 60 -and -not $up; $i++) {
    try { $ping = Invoke-RestMethod "$base/api/ping"; $up = $ping.app -eq "hoadon-kho" } catch { Start-Sleep -Milliseconds 500 }
  }
  Check $up "exe starts and answers on $base (version $($ping.version))"

  $xml = Join-Path $root "testdata\hoadon-mau.xml"
  $raw = curl.exe -s -H "X-App: hoadon-kho" -F "files=@$xml" "$base/api/parse"
  $draft = @(($raw | ConvertFrom-Json) | ForEach-Object { $_ })[0]
  Check (-not $draft.error -and $draft.invoice.lines.Count -eq 7) "XML invoice parsed (7 lines)"
  $bad = (Count ($draft.invoice.lines | ForEach-Object { $_.issues })) + (Count $draft.invoice.issues)
  Check ($bad -eq 0) "XML invoice fully verified (no open issues)"

  $body = @{ token = $draft.token; invoice = $draft.invoice } | ConvertTo-Json -Depth 12
  $saved = Invoke-RestMethod -Method Post -Uri "$base/api/invoices" -Headers $hdr -ContentType "application/json; charset=utf-8" -Body ([Text.Encoding]::UTF8.GetBytes($body))
  Check ($saved.receiptNo -eq "PN00001") "invoice saved, stock receipt $($saved.receiptNo)"

  $stock = @((Invoke-RestMethod "$base/api/stock") | ForEach-Object { $_ })
  $value = ($stock | Measure-Object -Property closeVal -Sum).Sum
  Check ($stock.Count -eq 7 -and $value -eq 72242593) "stock has 7 items worth $value"

  foreach ($k in "purchases", "receipts", "stock", "cards", "services", "import") {
    $out = Join-Path $data "$k.xlsx"
    Invoke-WebRequest "$base/api/export/$k`?from=2025-09-01&to=2025-09-30" -OutFile $out -UseBasicParsing
    $b = [IO.File]::ReadAllBytes($out)
    Check ($b.Length -gt 4000 -and $b[0] -eq 0x50 -and $b[1] -eq 0x4B) "Excel export '$k' ($($b.Length) bytes)"
  }

  $o = Invoke-RestMethod "$base/api/ocr"
  Write-Host "info OCR languages on this machine: $($o.langs -join ', ') (Vietnamese: $($o.vietnamese)) $($o.error)"
  $img = Join-Path $root "testdata\corpus\20a_png_photo_invoice.png"
  $r = & $exePath -parse $img | Out-String | ConvertFrom-Json
  if ($r.error) { Write-Host "info image OCR not available here: $($r.error)" }
  else { Check ($r.invoice.lines.Count -gt 0 -and (Count $r.problems) -gt 0) "image read by OCR and flagged for review ($(Count $r.problems) open points)" }
  Write-Host "SMOKE TEST PASSED"
}
finally {
  Stop-Process -Id $proc.Id -Force -ErrorAction SilentlyContinue
  Remove-Item -Recurse -Force $data -ErrorAction SilentlyContinue
}
