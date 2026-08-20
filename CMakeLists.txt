cmake_minimum_required(VERSION 4.0)
set(CMAKE_CXX_STANDARD 17)

project(fpga-host)

set(CMAKE_CXX_SCAN_FOR_MODULES 0)
set(CMAKE_EXPORT_COMPILE_COMMANDS ON)

# option(USE_CCACHE "Use ccache for compilation" ON)
# set(CCACHE_PATH "ccache" CACHE STRING "Path to ccache binary")
# if(USE_CCACHE)
#     find_program(CCACHE_BINARY ${CCACHE_PATH})
#     if(CCACHE_BINARY)
#         message(STATUS "[UseCCache] Found ccache at: ${CCACHE_BINARY}")
#         set(CMAKE_C_COMPILER_LAUNCHER ${CCACHE_BINARY})
#         set(CMAKE_CXX_COMPILER_LAUNCHER ${CCACHE_BINARY})
#     else()
#         message(FATAL_ERROR "[UseCCache] USE_CCACHE enabled, but no "
#             "executable found at: ${CCACHE_PATH}")
#     endif()
# endif()

include(cmake/CPM.cmake)
set(CPM_SOURCE_CACHE ${CMAKE_BINARY_DIR}/cpmcache)

add_subdirectory(server)
