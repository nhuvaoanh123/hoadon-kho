# OCR an image or PDF with the built-in Windows OCR engine; prints JSON words with boxes.
# Input path in env HOADONKHO_OCR_PATH; optional language tag in HOADONKHO_OCR_LANG.
$ErrorActionPreference = "Stop"
[Console]::OutputEncoding = [Text.Encoding]::UTF8
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
$null = [Windows.Storage.Streams.InMemoryRandomAccessStream, Windows.Storage.Streams, ContentType = WindowsRuntime]
$null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics, ContentType = WindowsRuntime]
$null = [Windows.Globalization.Language, Windows.Globalization, ContentType = WindowsRuntime]
$null = [Windows.Data.Pdf.PdfDocument, Windows.Data.Pdf, ContentType = WindowsRuntime]
$ext = [System.WindowsRuntimeSystemExtensions].GetMethods()
$asTaskT = ($ext | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1' })[0]
$asTaskA = ($ext | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncAction' })[0]
function Await($op, [Type]$t) { $task = $asTaskT.MakeGenericMethod($t).Invoke($null, @($op)); $task.Wait(); $task.Result }
function AwaitA($op) { $task = $asTaskA.Invoke($null, @($op)); $task.Wait() }

$langs = @([Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages | ForEach-Object { $_.LanguageTag })
$want = $env:HOADONKHO_OCR_LANG
$tag = if ($want -and $langs -contains $want) { $want } elseif ($langs -contains 'vi-VN') { 'vi-VN' } elseif ($langs -contains 'en-US') { 'en-US' } else { $langs[0] }
if (-not $tag) { throw "Windows chưa cài ngôn ngữ OCR nào" }
$eng = [Windows.Media.Ocr.OcrEngine]::TryCreateFromLanguage([Windows.Globalization.Language]::new($tag))

function Read-Bitmap($stream) {
  $dec = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
  $w = $dec.OrientedPixelWidth; $h = $dec.OrientedPixelHeight
  $scale = [math]::Min(3.0, [math]::Max(1.0, 2400.0 / [math]::Max($w, 1)))
  $scale = [math]::Min($scale, ([Windows.Media.Ocr.OcrEngine]::MaxImageDimension - 1) / [math]::Max($w, $h))
  $tf = [Windows.Graphics.Imaging.BitmapTransform]::new()
  $tf.ScaledWidth = [uint32]($w * $scale); $tf.ScaledHeight = [uint32]($h * $scale)
  $tf.InterpolationMode = [Windows.Graphics.Imaging.BitmapInterpolationMode]::Fant
  Await ($dec.GetSoftwareBitmapAsync([Windows.Graphics.Imaging.BitmapPixelFormat]::Bgra8,
    [Windows.Graphics.Imaging.BitmapAlphaMode]::Premultiplied, $tf,
    [Windows.Graphics.Imaging.ExifOrientationMode]::RespectExifOrientation,
    [Windows.Graphics.Imaging.ColorManagementMode]::DoNotColorManage)) ([Windows.Graphics.Imaging.SoftwareBitmap])
}

function Recognize($bmp, $page) {
  $res = Await ($eng.RecognizeAsync($bmp)) ([Windows.Media.Ocr.OcrResult])
  foreach ($l in $res.Lines) { foreach ($w in $l.Words) { $r = $w.BoundingRect
    [pscustomobject]@{ p = $page; t = $w.Text; x = [int]$r.X; y = [int]$r.Y; w = [int]$r.Width; h = [int]$r.Height } } }
}

if (-not $env:HOADONKHO_OCR_PATH) { # chỉ hỏi ngôn ngữ OCR đã cài
  [pscustomobject]@{ lang = $tag; langs = $langs; words = @() } | ConvertTo-Json -Depth 4 -Compress
  exit 0
}
$file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($env:HOADONKHO_OCR_PATH)) ([Windows.Storage.StorageFile])
$words = @()
if ($file.FileType -ieq '.pdf') {
  $doc = Await ([Windows.Data.Pdf.PdfDocument]::LoadFromFileAsync($file)) ([Windows.Data.Pdf.PdfDocument])
  for ($i = 0; $i -lt [math]::Min($doc.PageCount, 20); $i++) {
    $pg = $doc.GetPage($i)
    $ms = [Windows.Storage.Streams.InMemoryRandomAccessStream]::new()
    $opt = [Windows.Data.Pdf.PdfPageRenderOptions]::new()
    $opt.DestinationWidth = 2480
    AwaitA ($pg.RenderToStreamAsync($ms, $opt))
    $words += @(Recognize (Read-Bitmap $ms) $i)
  }
} else {
  $stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
  $words += @(Recognize (Read-Bitmap $stream) 0)
}
[pscustomobject]@{ lang = $tag; langs = $langs; words = $words } | ConvertTo-Json -Depth 4 -Compress
