package io.github.revise0592.mcpanel.ui.theme

import android.app.Activity
import android.os.Build
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.ColorScheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.dynamicDarkColorScheme
import androidx.compose.material3.dynamicLightColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.Immutable
import androidx.compose.runtime.SideEffect
import androidx.compose.runtime.staticCompositionLocalOf
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.compositeOver
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.core.view.WindowCompat
import io.github.revise0592.mcpanel.data.DEFAULT_ACCENT

/** The web UI's colours (internal/web/static/style.css), so a panel looks the same in the app. */
private object Web {
    val bg = Color(0xFFF4F5F7)
    val surface = Color(0xFFFFFFFF)
    val surface2 = Color(0xFFF0F2F5)
    val border = Color(0xFFE2E5EA)
    val text = Color(0xFF1A1E24)
    val muted = Color(0xFF5D6877)
    val idle = Color(0xFF8B95A3)

    val darkBg = Color(0xFF0E1115)
    val darkSurface = Color(0xFF161A20)
    val darkSurface2 = Color(0xFF1D222A)
    val darkBorder = Color(0xFF282E37)
    val darkText = Color(0xFFE5E8EC)
    val darkMuted = Color(0xFF98A1AD)

    val bad = Color(0xFFDC2626)
}

/** Colours outside Material's scheme: server states and the console. */
@Immutable
data class PanelColors(
    val ok: Color = Color(0xFF16A34A),
    val warn: Color = Color(0xFFD97706),
    val idle: Color = Web.idle,
    val consoleBg: Color = Color(0xFF11151A),
    val consoleText: Color = Color(0xFFD7DDE4),
    val consolePanel: Color = Color(0xFF7CC4FF),
    val consoleWarn: Color = Color(0xFFF5C26B),
    val consoleErr: Color = Color(0xFFFF8A80),
    val consoleCmd: Color = Color(0xFF9AE6B4),
)

val LocalPanelColors = staticCompositionLocalOf { PanelColors() }

/** Reads "#rrggbb", or returns null. */
fun parseHex(hex: String): Color? {
    val s = hex.trim().removePrefix("#")
    if (s.length != 6) return null
    return s.toLongOrNull(16)?.let { Color(0xFF000000 or it) }
}

/** Like CSS color-mix(in srgb, [c] [amount], [over]). */
private fun mix(c: Color, amount: Float, over: Color) = c.copy(alpha = amount).compositeOver(over)

/** A colour scheme built like the web UI's, around the panel's accent. */
fun panelColorScheme(accentHex: String, accentTextHex: String, dark: Boolean): ColorScheme {
    val accent = parseHex(accentHex) ?: parseHex(DEFAULT_ACCENT)!!
    val onAccent = parseHex(accentTextHex) ?: Color.White
    return if (dark) {
        val soft = mix(accent, 0.22f, Web.darkSurface)
        darkColorScheme(
            primary = accent, onPrimary = onAccent,
            primaryContainer = soft, onPrimaryContainer = Web.darkText,
            secondary = accent, onSecondary = onAccent,
            secondaryContainer = soft, onSecondaryContainer = Web.darkText,
            tertiary = accent, onTertiary = onAccent,
            tertiaryContainer = soft, onTertiaryContainer = Web.darkText,
            background = Web.darkBg, onBackground = Web.darkText,
            surface = Web.darkBg, onSurface = Web.darkText,
            surfaceVariant = Web.darkSurface2, onSurfaceVariant = Web.darkMuted,
            surfaceTint = accent,
            surfaceBright = Web.darkSurface2, surfaceDim = Web.darkBg,
            surfaceContainerLowest = Web.darkBg,
            surfaceContainerLow = Web.darkSurface,
            surfaceContainer = Web.darkSurface,
            surfaceContainerHigh = Web.darkSurface,
            surfaceContainerHighest = Web.darkSurface,
            outline = Web.darkMuted.copy(alpha = 0.6f).compositeOver(Web.darkSurface),
            outlineVariant = Web.darkBorder,
            error = Color(0xFFF87171), onError = Color(0xFF16191D),
            errorContainer = mix(Web.bad, 0.18f, Web.darkSurface), onErrorContainer = Color(0xFFFCA5A5),
            inverseSurface = Web.darkText, inverseOnSurface = Web.darkBg, inversePrimary = accent,
        )
    } else {
        val soft = mix(accent, 0.12f, Web.surface)
        lightColorScheme(
            primary = accent, onPrimary = onAccent,
            primaryContainer = soft, onPrimaryContainer = Web.text,
            secondary = accent, onSecondary = onAccent,
            secondaryContainer = soft, onSecondaryContainer = Web.text,
            tertiary = accent, onTertiary = onAccent,
            tertiaryContainer = soft, onTertiaryContainer = Web.text,
            background = Web.bg, onBackground = Web.text,
            surface = Web.bg, onSurface = Web.text,
            surfaceVariant = Web.surface2, onSurfaceVariant = Web.muted,
            surfaceTint = accent,
            surfaceBright = Web.surface, surfaceDim = Web.surface2,
            surfaceContainerLowest = Web.surface,
            surfaceContainerLow = Web.surface,
            surfaceContainer = Web.surface,
            surfaceContainerHigh = Web.surface,
            surfaceContainerHighest = Web.surface,
            outline = Web.idle,
            outlineVariant = Web.border,
            error = Web.bad, onError = Color.White,
            errorContainer = mix(Web.bad, 0.10f, Web.surface), onErrorContainer = Color(0xFF991B1B),
            inverseSurface = Web.text, inverseOnSurface = Web.bg, inversePrimary = accent,
        )
    }
}

/**
 * The app's theme. With a [panel] accent the screens look like that
 * panel's web UI, including its light or dark choice; without one (the
 * panel list) the phone's own wallpaper colours are used.
 */
@Composable
fun McPanelTheme(
    accent: String? = null,
    accentText: String = "#ffffff",
    theme: String = "system",
    content: @Composable () -> Unit,
) {
    val dark = when (theme) {
        "light" -> false
        "dark" -> true
        else -> isSystemInDarkTheme()
    }
    val colors = when {
        accent != null -> panelColorScheme(accent, accentText, dark)
        Build.VERSION.SDK_INT >= Build.VERSION_CODES.S -> {
            val context = LocalContext.current
            if (dark) dynamicDarkColorScheme(context) else dynamicLightColorScheme(context)
        }
        else -> panelColorScheme(DEFAULT_ACCENT, "#ffffff", dark)
    }
    // A panel set to light or dark can differ from the phone, so the status
    // bar icons have to follow the panel.
    val view = LocalView.current
    if (!view.isInEditMode) {
        SideEffect {
            val window = (view.context as? Activity)?.window ?: return@SideEffect
            WindowCompat.getInsetsController(window, view).apply {
                isAppearanceLightStatusBars = !dark
                isAppearanceLightNavigationBars = !dark
            }
        }
    }
    CompositionLocalProvider(LocalPanelColors provides PanelColors()) {
        MaterialTheme(colorScheme = colors, content = content)
    }
}
