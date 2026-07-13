@echo off
setlocal
set "ELECTRON_RUN_AS_NODE=1"

set "FRAGFORGE_MCP_ELECTRON=%~dp0FragForge Studio.exe"
set "FRAGFORGE_MCP_ENTRY=%~dp0resources\app.asar\dist\mcp\stdio.js"
set "FRAGFORGE_MCP_INSTALLED=1"
if exist "%FRAGFORGE_MCP_ELECTRON%" goto run

set "FRAGFORGE_MCP_INSTALLED=0"
set "FRAGFORGE_MCP_ELECTRON=%~dp0..\node_modules\electron\dist\electron.exe"
set "FRAGFORGE_MCP_ENTRY=%~dp0..\dist\mcp\stdio.js"

:run
if not exist "%FRAGFORGE_MCP_ELECTRON%" (
  1>&2 echo FragForge MCP could not find the Electron runtime
  exit /b 1
)
if "%FRAGFORGE_MCP_INSTALLED%"=="0" if not exist "%FRAGFORGE_MCP_ENTRY%" (
  1>&2 echo FragForge MCP could not find the compiled server; run npm run build
  exit /b 1
)

"%FRAGFORGE_MCP_ELECTRON%" "%FRAGFORGE_MCP_ENTRY%"
exit /b %errorlevel%
