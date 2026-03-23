package main

// ICO format icons (32x32, embedded as byte arrays)
// Three states: blue (default), green (running), red (stopped)

var (
	trayIcon      []byte
	trayIconGreen []byte
	trayIconRed   []byte
)

func init() {
	trayIcon = makeIcon(59, 130, 246)     // blue
	trayIconGreen = makeIcon(34, 197, 94) // green
	trayIconRed = makeIcon(220, 38, 38)   // red
}

func makeIcon(r, g, b byte) []byte {
	width, height := 32, 32

	// BMP data (BGRA, bottom-up)
	pixels := make([]byte, width*height*4)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// Normalized coordinates (bottom-up for BMP)
			fy := float64(height-1-y) / float64(height)
			fx := float64(x) / float64(width)
			cx := 0.5
			dx := fx - cx
			if dx < 0 {
				dx = -dx
			}

			// Shield shape boundary
			shieldDist := shieldDistance(fx, fy)

			idx := (y*width + x) * 4
			if shieldDist < -0.02 {
				// Inner area - lighter shade for depth
				innerDist := shieldDistance(fx, fy) + 0.08
				if innerDist < -0.02 {
					// Inner highlight (lighter)
					lr := clampByte(int(r) + 40)
					lg := clampByte(int(g) + 40)
					lb := clampByte(int(b) + 40)
					pixels[idx+0] = lb // B
					pixels[idx+1] = lg // G
					pixels[idx+2] = lr // R
					pixels[idx+3] = 255
				} else if innerDist < 0.0 {
					// Inner edge blend
					t := (innerDist + 0.02) / 0.02
					lr := clampByte(int(r) + int(float64(40)*(1.0-t)))
					lg := clampByte(int(g) + int(float64(40)*(1.0-t)))
					lb := clampByte(int(b) + int(float64(40)*(1.0-t)))
					pixels[idx+0] = lb
					pixels[idx+1] = lg
					pixels[idx+2] = lr
					pixels[idx+3] = 255
				} else {
					// Outer part of shield body
					pixels[idx+0] = b
					pixels[idx+1] = g
					pixels[idx+2] = r
					pixels[idx+3] = 255
				}
			} else if shieldDist < 0.02 {
				// Anti-aliased edge: blend shield color with transparent
				alpha := 1.0 - (shieldDist+0.02)/0.04
				if alpha < 0 {
					alpha = 0
				}
				if alpha > 1 {
					alpha = 1
				}
				a := byte(alpha * 255)
				pixels[idx+0] = b
				pixels[idx+1] = g
				pixels[idx+2] = r
				pixels[idx+3] = a
			} else {
				// Outside shield - transparent
				pixels[idx+0] = 0
				pixels[idx+1] = 0
				pixels[idx+2] = 0
				pixels[idx+3] = 0
			}
		}
	}

	// ICO file format
	ico := []byte{
		// ICONDIR
		0, 0, // reserved
		1, 0, // type: icon
		1, 0, // count: 1
		// ICONDIRENTRY
		byte(width), byte(height), // size
		0,    // colors
		0,    // reserved
		1, 0, // planes
		32, 0, // bpp
		0, 0, 0, 0, // size (filled below)
		22, 0, 0, 0, // offset to BMP
	}

	// BITMAPINFOHEADER
	bmpHeader := []byte{
		40, 0, 0, 0, // header size
		byte(width), 0, 0, 0, // width
		byte(height * 2), 0, 0, 0, // height (doubled for AND mask)
		1, 0, // planes
		32, 0, // bpp
		0, 0, 0, 0, // compression
		0, 0, 0, 0, // image size
		0, 0, 0, 0, // x ppm
		0, 0, 0, 0, // y ppm
		0, 0, 0, 0, // colors used
		0, 0, 0, 0, // important colors
	}

	// AND mask (all zeros = fully visible where alpha says so)
	// 32 pixels wide = 4 bytes per row, already aligned to 4 bytes
	andMask := make([]byte, height*4)

	data := append(bmpHeader, pixels...)
	data = append(data, andMask...)

	// Fill size in ICONDIRENTRY
	size := len(data)
	ico[14] = byte(size)
	ico[15] = byte(size >> 8)
	ico[16] = byte(size >> 16)
	ico[17] = byte(size >> 24)

	return append(ico, data...)
}

// shieldDistance returns signed distance to shield boundary.
// Negative = inside shield, positive = outside.
func shieldDistance(fx, fy float64) float64 {
	cx := 0.5
	dx := fx - cx
	if dx < 0 {
		dx = -dx
	}

	// Shield shape: wide at top, narrows to point at bottom
	if fy < 0.12 || fy > 0.95 {
		return 1.0 // clearly outside
	}

	// Maximum half-width at this y position
	maxW := 0.42 - (fy-0.5)*0.35
	if fy > 0.7 {
		// Taper more aggressively near the bottom point
		maxW = 0.42 - (0.7-0.5)*0.35 - (fy-0.7)*0.8
	}
	if maxW < 0.02 {
		maxW = 0.02
	}

	return dx - maxW
}

func clampByte(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}
