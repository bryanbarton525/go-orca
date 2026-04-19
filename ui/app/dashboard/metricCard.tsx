import React from 'react';

export default function MetricCard({ title, value }: { title: string; value: number }) {
  return (
    <div className='bg-white p-4 rounded shadow-md'>
      <h2 className='text-xl font-bold'>{title}</h2>
      <p className='text-3xl mt-2'>{value}</p>
    </div>
  );
}