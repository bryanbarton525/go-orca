import React from 'react';

export default function SideBar() {
  return (
    <div className='w-64 bg-gray-800 text-white min-h-screen'>
      <ul>
        <li className='p-2 hover:bg-gray-700 cursor-pointer'>Dashboard</li>
        <li className='p-2 hover:bg-gray-700 cursor-pointer'>Settings</li>
      </ul>
    </div>
  );
}