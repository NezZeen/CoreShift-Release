# Google's code scanner (MainActivity.scanQr) reads its internal messages
# by reflection; R8 in full mode strips their fields and the scanner fails
# with a NullPointerException before the camera opens.
-keep class com.google.android.gms.internal.mlkit_code_scanner.** { *; }
-keep class com.google.mlkit.** { *; }
