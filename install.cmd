@echo off
rem honjoji installer: no admin needed. Usage: install.cmd [-NoRun] [-Uninstall]
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0install.ps1" %*
