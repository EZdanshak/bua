package browser

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// StealthConfig configures anti-detection measures.
type StealthConfig struct {
	// EnableStealth enables stealth mode with anti-detection.
	EnableStealth bool

	// UserAgent overrides the browser user agent.
	UserAgent string

	// Locale sets the browser locale (e.g., "en-US").
	Locale string

	// Timezone sets the browser timezone (e.g., "America/New_York").
	Timezone string

	// WebGLVendor spoofs the WebGL vendor.
	WebGLVendor string

	// WebGLRenderer spoofs the WebGL renderer.
	WebGLRenderer string

	// HumanLikeDelays adds random delays between actions.
	HumanLikeDelays bool

	// MinDelay minimum delay between actions (ms).
	MinDelay int

	// MaxDelay maximum delay between actions (ms).
	MaxDelay int
}

// DefaultStealthConfig returns sensible stealth defaults.
func DefaultStealthConfig() StealthConfig {
	return StealthConfig{
		EnableStealth:   true,
		UserAgent:       "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36",
		Locale:          "en-US",
		Timezone:        "America/Los_Angeles",
		WebGLVendor:     "Google Inc. (Apple)",
		WebGLRenderer:   "ANGLE (Apple, ANGLE Metal Renderer: Apple M2 Pro, Unspecified Version)",
		HumanLikeDelays: true,
		MinDelay:        50,
		MaxDelay:        150,
	}
}

// stealthJS is the JavaScript code injected to evade bot detection.
// Based on puppeteer-extra-plugin-stealth and go-rod/stealth.
const stealthJS = `
(() => {
    // 1. Override navigator.webdriver
    Object.defineProperty(navigator, 'webdriver', {
        get: () => undefined,
        configurable: true
    });

    // 2. Override navigator.plugins to appear normal
    const mockPlugins = {
        length: 5,
        item: (index) => mockPlugins[index],
        namedItem: (name) => null,
        refresh: () => {},
        0: { name: 'Chrome PDF Plugin', description: 'Portable Document Format', filename: 'internal-pdf-viewer', length: 1 },
        1: { name: 'Chrome PDF Viewer', description: '', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', length: 1 },
        2: { name: 'Native Client', description: '', filename: 'internal-nacl-plugin', length: 2 },
        3: { name: 'Chromium PDF Plugin', description: 'Portable Document Format', filename: 'internal-pdf-viewer', length: 1 },
        4: { name: 'Chromium PDF Viewer', description: '', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', length: 1 }
    };
    Object.defineProperty(navigator, 'plugins', {
        get: () => mockPlugins,
        configurable: true
    });

    // 3. Override navigator.languages
    Object.defineProperty(navigator, 'languages', {
        get: () => ['en-US', 'en'],
        configurable: true
    });

    // 4. Override navigator.permissions.query
    const originalQuery = navigator.permissions.query;
    navigator.permissions.query = (parameters) => {
        if (parameters.name === 'notifications') {
            return Promise.resolve({ state: Notification.permission });
        }
        return originalQuery(parameters);
    };

    // 5. Override chrome.runtime to appear like regular Chrome
    if (!window.chrome) {
        window.chrome = {};
    }
    if (!window.chrome.runtime) {
        window.chrome.runtime = {
            connect: () => {},
            sendMessage: () => {},
            onMessage: { addListener: () => {}, removeListener: () => {} },
            onConnect: { addListener: () => {}, removeListener: () => {} },
            PlatformOs: { MAC: 'mac', WIN: 'win', ANDROID: 'android', CROS: 'cros', LINUX: 'linux', OPENBSD: 'openbsd' },
            PlatformArch: { ARM: 'arm', X86_32: 'x86-32', X86_64: 'x86-64', MIPS: 'mips', MIPS64: 'mips64' },
            PlatformNaclArch: { ARM: 'arm', X86_32: 'x86-32', X86_64: 'x86-64', MIPS: 'mips', MIPS64: 'mips64' },
            RequestUpdateCheckStatus: { THROTTLED: 'throttled', NO_UPDATE: 'no_update', UPDATE_AVAILABLE: 'update_available' },
            OnInstalledReason: { INSTALL: 'install', UPDATE: 'update', CHROME_UPDATE: 'chrome_update', SHARED_MODULE_UPDATE: 'shared_module_update' },
            OnRestartRequiredReason: { APP_UPDATE: 'app_update', OS_UPDATE: 'os_update', PERIODIC: 'periodic' }
        };
    }
    if (!window.chrome.app) {
        window.chrome.app = {
            isInstalled: false,
            InstallState: { DISABLED: 'disabled', INSTALLED: 'installed', NOT_INSTALLED: 'not_installed' },
            RunningState: { CANNOT_RUN: 'cannot_run', READY_TO_RUN: 'ready_to_run', RUNNING: 'running' },
            getDetails: () => null,
            getIsInstalled: () => false,
            runningState: () => 'cannot_run'
        };
    }
    if (!window.chrome.csi) {
        window.chrome.csi = () => ({
            startE: Date.now(),
            onloadT: Date.now(),
            pageT: Math.random() * 1000 + 500,
            tran: 15
        });
    }
    if (!window.chrome.loadTimes) {
        window.chrome.loadTimes = () => ({
            commitLoadTime: Date.now() / 1000,
            connectionInfo: 'h2',
            finishDocumentLoadTime: Date.now() / 1000 + 0.5,
            finishLoadTime: Date.now() / 1000 + 0.7,
            firstPaintAfterLoadTime: Date.now() / 1000 + 0.8,
            firstPaintTime: Date.now() / 1000 + 0.3,
            navigationType: 'Other',
            npnNegotiatedProtocol: 'h2',
            requestTime: Date.now() / 1000 - 0.5,
            startLoadTime: Date.now() / 1000 - 0.3,
            wasAlternateProtocolAvailable: false,
            wasFetchedViaSpdy: true,
            wasNpnNegotiated: true
        });
    }

    // 6. Hide automation-related properties
    delete navigator.__proto__.webdriver;

    // 7. Spoof screen properties
    Object.defineProperty(screen, 'availWidth', { get: () => window.innerWidth });
    Object.defineProperty(screen, 'availHeight', { get: () => window.innerHeight });

    // 8. Override toString() to hide proxy objects
    const originalFunction = Function.prototype.toString;
    const proxyHandler = {
        apply: function(target, thisArg, argumentsList) {
            if (thisArg === navigator.permissions.query) {
                return 'function query() { [native code] }';
            }
            return originalFunction.apply(thisArg, argumentsList);
        }
    };
    Function.prototype.toString = new Proxy(Function.prototype.toString, proxyHandler);

    // 9. Remove headless indicators
    Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });
    Object.defineProperty(navigator, 'deviceMemory', { get: () => 8 });
    Object.defineProperty(navigator, 'maxTouchPoints', { get: () => 0 });

    // 10. Mock battery API if needed
    if (navigator.getBattery) {
        navigator.getBattery = () => Promise.resolve({
            charging: true,
            chargingTime: 0,
            dischargingTime: Infinity,
            level: 1,
            addEventListener: () => {},
            removeEventListener: () => {}
        });
    }

    console.log('[Stealth] Anti-detection measures applied');
})();
`

// applyStealthMode injects stealth JavaScript into a page.
func applyStealthMode(page *rod.Page, cfg StealthConfig) error {
	if !cfg.EnableStealth {
		return nil
	}

	// Set user agent if specified
	if cfg.UserAgent != "" {
		if err := page.SetUserAgent(&proto.NetworkSetUserAgentOverride{
			UserAgent:      cfg.UserAgent,
			AcceptLanguage: cfg.Locale,
		}); err != nil {
			return fmt.Errorf("failed to set user agent: %w", err)
		}
	}

	// Set timezone if specified using CDP directly
	if cfg.Timezone != "" {
		err := proto.EmulationSetTimezoneOverride{
			TimezoneID: cfg.Timezone,
		}.Call(page)
		if err != nil {
			// Continue even if timezone fails - not critical
		}
	}

	// Build WebGL override script
	webglScript := ""
	if cfg.WebGLVendor != "" || cfg.WebGLRenderer != "" {
		webglScript = fmt.Sprintf(`
(() => {
    const getParameter = WebGLRenderingContext.prototype.getParameter;
    WebGLRenderingContext.prototype.getParameter = function(parameter) {
        if (parameter === 37445) return '%s';
        if (parameter === 37446) return '%s';
        return getParameter.call(this, parameter);
    };
    const getParameter2 = WebGL2RenderingContext.prototype.getParameter;
    WebGL2RenderingContext.prototype.getParameter = function(parameter) {
        if (parameter === 37445) return '%s';
        if (parameter === 37446) return '%s';
        return getParameter2.call(this, parameter);
    };
})();
`, cfg.WebGLVendor, cfg.WebGLRenderer, cfg.WebGLVendor, cfg.WebGLRenderer)
	}

	// Combine all stealth scripts
	fullScript := stealthJS + webglScript

	// Inject stealth scripts on every navigation
	// Using EvalOnNewDocument ensures it runs before any page script
	_, err := page.EvalOnNewDocument(fullScript)
	if err != nil {
		return fmt.Errorf("failed to inject stealth script: %w", err)
	}

	return nil
}

// humanDelay adds a random human-like delay.
func humanDelay(minMs, maxMs int) {
	if minMs <= 0 || maxMs <= minMs {
		return
	}

	diff := int64(maxMs - minMs)
	n, err := rand.Int(rand.Reader, big.NewInt(diff))
	if err != nil {
		time.Sleep(time.Duration(minMs) * time.Millisecond)
		return
	}

	delay := time.Duration(minMs+int(n.Int64())) * time.Millisecond
	time.Sleep(delay)
}

// randomMouseOffset returns a small random offset for more human-like clicking.
func randomMouseOffset(maxOffset float64) (float64, float64) {
	xN, _ := rand.Int(rand.Reader, big.NewInt(int64(maxOffset*2)))
	yN, _ := rand.Int(rand.Reader, big.NewInt(int64(maxOffset*2)))

	x := float64(xN.Int64()) - maxOffset
	y := float64(yN.Int64()) - maxOffset

	return x, y
}

// Additional launch flags for stealth mode.
var stealthLaunchFlags = []string{
	"disable-blink-features=AutomationControlled",      // Most important: hides webdriver
	"disable-infobars",                                 // Remove "Chrome is being controlled" bar
	"disable-dev-shm-usage",                            // Prevent shared memory issues
	"disable-ipc-flooding-protection",                  // Performance
	"disable-renderer-backgrounding",                   // Keep renderers active
	"disable-backgrounding-occluded-windows",           // Keep background windows active
	"disable-background-timer-throttling",              // Keep timers active
	"disable-features=IsolateOrigins,site-per-process", // Reduce isolation overhead
	"no-sandbox",                     // Required for some environments
	"ignore-certificate-errors",      // Ignore SSL errors
	"allow-running-insecure-content", // Allow mixed content
	"disable-web-security",           // Disable CORS (use carefully)
}

// GetStealthLaunchFlags returns Chrome flags for stealth mode.
func GetStealthLaunchFlags() []string {
	return stealthLaunchFlags
}
